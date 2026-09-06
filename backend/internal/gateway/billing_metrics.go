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

// notifySettled 成功终态投递 usage:write + billing:settle（best-effort：投递失败不阻断响应，
// 由 settle 侧 WAL/重试与 reserve:reclaim 兜底，03 §4.5）。
func (g *Gateway) notifySettled(ctx context.Context, c *Caller, at *router.Attempt, req *ir.CanonicalRequest, price *model.ModelPrice, resp *ir.CanonicalResponse, requestID string) {
	if g.Prod == nil || requestID == "" {
		return
	}
	channelRate := at.Candidate.Channel.RateMultiplier.Decimal
	snap := priceSnapshot(price, c.Group.RateMultiplier.Decimal, channelRate)
	usage := resp.Usage
	tokens := billing.Tokens{Input: usage.InputTokens, Output: usage.OutputTokens, CacheRead: usage.CacheReadTokens}
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
		Stream:        false,
		StatusCode:    200,
		CreatedAt:     now,
	}
	if err := g.Prod.SendUsage(ctx, usagePayload); err != nil {
		_ = err // best-effort；日志留痕由调用方（handler）负责
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
		_ = err
	}
}
