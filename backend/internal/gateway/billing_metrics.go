package gateway

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"cloudfog/internal/billing"
	"cloudfog/internal/ir"
	"cloudfog/internal/model"
	"cloudfog/internal/router"
)

func decOrZero(d *model.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return d.Decimal
}

// priceSnapshot 由模型当前生效价构造可传输快照（rate=分组倍率×渠道倍率，06 §2.2 口径）。
func priceSnapshot(p *model.ModelPrice, groupRate, channelRate decimal.Decimal) billing.PriceSnapshot {
	snap := billing.PriceSnapshot{RateMultiplier: groupRate.Mul(channelRate)}
	if p == nil {
		return snap
	}
	snap.ModelID = p.ModelID
	snap.Currency = p.Currency
	snap.InputPer1K = p.InputPricePer1K.Decimal
	snap.OutputPer1K = p.OutputPricePer1K.Decimal
	snap.CacheReadPer1K = decOrZero(p.CacheReadPricePer1K)
	snap.CacheWritePer1K = decOrZero(p.CacheWritePricePer1K)
	snap.PerRequest = p.PerRequestPrice.Decimal
	return snap
}

// estimateTokens 本地粗估（06 §3.1 预估的 MVP 简化：文本按 ~4 字符/token，无 tokenizer）。
func estimateTokens(req *ir.CanonicalRequest) billing.Tokens {
	chars := 0
	for _, m := range req.Messages {
		for _, part := range m.Content {
			if part.Type == ir.PartText {
				chars += len(part.Text)
			}
		}
	}
	chars += len(req.System)
	if chars == 0 {
		chars = 4
	}
	out := 2048 // 无 max_tokens 时的保守估计上限
	if v := req.Params.MaxTokens; v != nil && *v > 0 {
		out = *v
	}
	return billing.Tokens{Input: chars/4 + 1, Output: out}
}

// estimateCost 依据快照与估计用量计算预扣金额。
func estimateCost(snap billing.PriceSnapshot, req *ir.CanonicalRequest) decimal.Decimal {
	return billing.ComputeCost(snap, estimateTokens(req), false)
}

// StreamUsage 流式终态用量（出口层在流结束时回填给 Settle 钩子）。
type StreamUsage struct {
	Usage      *ir.Usage // 适配器上报的上游用量（stream_options.include_usage；可能 nil/零值）
	DeltaChars int       // 累计输出字符数（无上游 usage 时用于估算输出 token）
}

// resolveStreamTokens 流式用量归集：优先上游 usage；缺失时按输入字符粗估输入、
// 按已下发输出字符数粗估输出（≈4 字符/token），避免"零用量白嫖"。
func resolveStreamTokens(su StreamUsage, req *ir.CanonicalRequest) billing.Tokens {
	if su.Usage != nil && (su.Usage.InputTokens > 0 || su.Usage.OutputTokens > 0) {
		return billing.Tokens{
			Input:     su.Usage.InputTokens,
			Output:    su.Usage.OutputTokens,
			CacheRead: su.Usage.CacheReadTokens,
		}
	}
	in := estimateTokens(req)
	out := su.DeltaChars / 4
	if out == 0 && su.DeltaChars > 0 {
		out = 1
	}
	return billing.Tokens{Input: in.Input, Output: out}
}

// streamSettler 构造流式计量钩子（一次流仅调用一次；幂等键与非流同为 request_id）。
// statusCode/errCode 由出口层给出（200 完成 / 502 上游异常 / 断连等）。
func (g *Gateway) streamSettler(ctx context.Context, c *Caller, at *router.Attempt, req *ir.CanonicalRequest,
	price *model.ModelPrice, requestID string) func(StreamUsage, int, string) {
	return func(su StreamUsage, statusCode int, errCode string) {
		g.emitMetering(ctx, c, at, req, price, resolveStreamTokens(su, req), requestID, true, statusCode, errCode)
	}
}

// emitMetering 终态计量投递：usage:write + billing:settle（03 §4.5）。
// best-effort：投递失败不阻断响应，由 settle 侧 WAL/重试与 reserve:reclaim 兜底；
// **失败必须显式留痕**——此前错误被静默丢弃（_ = err），非流预扣会冻结至 45min reclaim
// 阈值且无人知晓（2026-09-08 审计修复）。
func (g *Gateway) emitMetering(ctx context.Context, c *Caller, at *router.Attempt, req *ir.CanonicalRequest,
	price *model.ModelPrice, tokens billing.Tokens, requestID string, stream bool, statusCode int, errCode string) {
	if g.Prod == nil || requestID == "" {
		return
	}
	channelRate := at.Candidate.Channel.RateMultiplier.Decimal
	snap := priceSnapshot(price, c.Group.RateMultiplier.Decimal, channelRate)
	cost := billing.ComputeCost(snap, tokens, false)

	now := time.Now().UTC()
	usagePayload := &billing.UsageLogPayload{
		RequestID: requestID, UserID: c.UserID, APIKeyID: c.APIKeyID,
		ChannelID:     at.Candidate.Channel.ID,
		Model:         req.Model,
		UpstreamModel: at.UpstreamModel,
		ProviderCode:  at.Candidate.Provider.Code,
		Tokens:        tokens,
		TotalCost:     cost,
		PriceSnapshot: snap,
		Stream:        stream,
		StatusCode:    statusCode,
		ErrorCode:     errCode,
		CreatedAt:     now,
	}
	if err := g.Prod.SendUsage(ctx, usagePayload); err != nil {
		g.log().Error("计量投递失败：usage:write 未入队（用量明细将缺失）",
			"request_id", requestID, "user_id", c.UserID, "error", err)
	}
	settle := &billing.SettlePayload{
		RequestID: requestID, UserID: c.UserID, APIKeyID: c.APIKeyID,
		ChannelID:     at.Candidate.Channel.ID,
		Model:         req.Model,
		UpstreamModel: at.UpstreamModel,
		Tokens:        tokens,
		PriceSnapshot: snap,
	}
	if err := g.Prod.SendSettle(ctx, settle); err != nil {
		g.log().Error("结算投递失败：billing:settle 未入队（非流预扣将冻结至 reclaim 阈值后归还）",
			"request_id", requestID, "user_id", c.UserID, "error", err)
	}
}

// notifySettled 非流成功终态计量（usage + settle）。
func (g *Gateway) notifySettled(ctx context.Context, c *Caller, at *router.Attempt, req *ir.CanonicalRequest,
	price *model.ModelPrice, resp *ir.CanonicalResponse, requestID string) {
	usage := resp.Usage
	g.emitMetering(ctx, c, at, req, price,
		billing.Tokens{Input: usage.InputTokens, Output: usage.OutputTokens, CacheRead: usage.CacheReadTokens},
		requestID, false, 200, "")
}
