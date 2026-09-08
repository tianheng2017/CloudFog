// Package gateway 请求编排（03 §4：归一化→路由→预扣→转发→计量）。
// 职责边界：预检（模型白名单/余额 402）→ 路由候选（router）→ 逐 attempt 转发
// （故障转移循环 03 §5）→ 计量/结算投递钩子（b2-6 接线真实 payload）。
// 本包无 DB 直连，仅依赖 consumer-side 接口，便于 httptest 假上游端到端验证。
package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"cloudfog/internal/billing"
	"cloudfog/internal/ir"
	"cloudfog/internal/model"
	"cloudfog/internal/pkg/adapter"
	"cloudfog/internal/pkg/crypto"
	"cloudfog/internal/router"
)

// BalanceStore 余额读取（预检 402）。
type BalanceStore interface {
	BalanceByUserID(ctx context.Context, userID int64) (*model.UserBalance, error)
}

// ModelLister 模型目录读取（GET /v1/models）。
type ModelLister interface {
	ListActiveModels(ctx context.Context) ([]model.Model, error)
}

// Caller 一次请求的鉴权主体摘要（由 API 层从 auth.Principal 填入）。
type Caller struct {
	UserID         int64
	APIKeyID       int64
	Group          *model.Group // 含 allowed_models / fallback_models
	ModelWhitelist []string     // 密钥级白名单（覆盖分组限制）
}

// Result 转发结果：非流（Resp）或流（Events）。
// 流式时调用方必须 defer Close()（关闭上游响应体，触发解析 goroutine 退出，04 §2 契约）。
type Result struct {
	Stream  bool
	Resp    *ir.CanonicalResponse
	Events  <-chan ir.StreamEvent
	Close   func() // 流式上游 body 关闭（幂等）
	Channel *router.Attempt
	// Settle 流式计量钩子（非流为 nil）：出口层在流结束时以累计用量调用**一次**，
	// 投递 usage:write + billing:settle（与非流同源同公式，03 §4.5）。
	// 未启用计量（Prod 为空或 requestID 为空）时为 nil，出口层须判空。
	Settle func(StreamUsage, int, string)
}

// Gateway 编排器（无状态，可并发；HTTP client 可注入便于测试/超时控制）。
type Gateway struct {
	Cat  router.Catalog
	Bal  BalanceStore
	List ModelLister
	HTTP *http.Client
	Now  func() time.Time
	// 计量扩展（b2-6）：Res=Redis 原子预扣；Prod=结算投递（均 nil 则跳过——测试/纯转发场景）
	Res interface {
		Reserve(ctx context.Context, userID int64, requestID string, amount decimal.Decimal) error
		Reset(ctx context.Context, userID int64, requestID string) error
	}
	Prod *billing.Producer
	// 凭证信封主密钥（08 §2，security.master_key/previous_master_key）：渠道凭证为密封形态时
	// 转发前解密；空 = 不支持密封凭证（仅 legacy 明文渠道可用，缺失时明确报错而非静默空凭据）。
	CredMK     string
	CredMKPrev string
	// 日志（可选）：计量/结算投递失败等资金链路异常必须显式留痕（缺省 slog.Default）。
	Log *slog.Logger
}

func (g *Gateway) log() *slog.Logger {
	if g.Log != nil {
		return g.Log
	}
	return slog.Default()
}

// Models 目录中全部可用模型（供 /v1/models）。
func (g *Gateway) Models(ctx context.Context) ([]model.Model, error) {
	if g.List == nil {
		return nil, nil
	}
	return g.List.ListActiveModels(ctx)
}

func (g *Gateway) httpClient() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return &http.Client{Timeout: 120 * time.Second}
}

// allowedModel 白名单判定：密钥级 ModelWhitelist（非空则覆盖）> 分组 AllowedModels（非空则限制）> 不限。
func allowedModel(whitelist, groupAllowed []string, name string) bool {
	if len(whitelist) > 0 {
		for _, m := range whitelist {
			if m == name {
				return true
			}
		}
		return false
	}
	if len(groupAllowed) > 0 {
		for _, m := range groupAllowed {
			if m == name {
				return true
			}
		}
		return false
	}
	return true
}

// Chat 编排一次对话请求（含预检与故障转移），成功返回 Result。
// requestID 为请求幂等/审计主键（usage/settle 幂等键与 Redis 审计键共用）。
func (g *Gateway) Chat(ctx context.Context, c *Caller, req *ir.CanonicalRequest, requestID string) (*Result, error) {
	if c == nil || c.Group == nil {
		return nil, ErrInvalidRequest("调用上下文缺失")
	}
	// fail-closed：启用 Redis 预扣即代表依赖结算投递——Producer 缺失（broker 启动时不可用）
	// 时不得放行计费请求（含流式：流式同样计量结算，否则为白嫖），
	// 否则预扣永不释放/漏计费（b2-7 拉通审查发现；2026-09-08 扩展至流式）。
	if g.Res != nil && g.Prod == nil {
		return nil, ErrBillingUnavailable()
	}
	// 1) 模型解析：目录中须存在且可对外（deprecated/hidden 不可用）
	res, err := g.ResolveModel(ctx, req.Model)
	if err != nil {
		return nil, err
	}
	if res.Model.Status != "active" {
		return nil, ErrModelNotFound(req.Model)
	}
	if !allowedModel(c.ModelWhitelist, c.Group.AllowedModels, req.Model) {
		return nil, ErrModelNotAllowed(req.Model)
	}
	// 2) 余额可用性：Redis 预扣未启用时退回 DB 预检（M5：不足 402，且不发起上游请求）
	reserved := false
	groupRate := c.Group.RateMultiplier.Decimal
	if g.Res == nil || req.Stream {
		// 流式计量/预扣释放为后续接入：流式请求仅做 DB 余额预检，不预扣（避免冻结滞留）
		if bal, err := g.Bal.BalanceByUserID(ctx, c.UserID); err != nil {
			return nil, err
		} else if bal == nil || !bal.Balance.GreaterThan(decimal.Zero) {
			return nil, ErrInsufficientBalance()
		}
	} else {
		// 3a) 非流 + Redis 预扣启用：按快照估计冻结（06 §3.1 简化版：预估用量）
		snap := priceSnapshot(res.Price, groupRate, decimal.NewFromInt(1))
		est := estimateCost(snap, req)
		if est.IsPositive() {
			if err := g.Res.Reserve(ctx, c.UserID, requestID, est); err != nil {
				if errors.Is(err, billing.ErrInsufficient) {
					return nil, ErrInsufficientBalance()
				}
				return nil, err
			}
			reserved = true
		}
	}
	releaseRes := func() {
		if reserved && requestID != "" {
			_ = g.Res.Reset(ctx, c.UserID, requestID)
			reserved = false
		}
	}

	// 3b) 降级链（05 §9：分组级 > 模型级）→ 路由候选
	chain := fallbackChain(req.Model, c.Group.FallbackModels, res.Model.Fallbacks)
	attempts, err := g.Plan(ctx, c.Group.ID, chain)
	if err != nil {
		releaseRes()
		return nil, err
	}
	if len(attempts) == 0 {
		releaseRes()
		return nil, ErrNoChannel()
	}

	var lastErr error
	for _, at := range attempts {
		if err := ctx.Err(); err != nil {
			releaseRes()
			return nil, err
		}
		if req.Stream {
			r, done, err := g.tryStream(ctx, at, req)
			if err == nil {
				// 流式计量：终态由出口层（流结束/断连/上游错误）调用 Settle 投递，
				// 与非流共用 emitMetering（同快照、同公式，06 §2.2）。
				return &Result{Stream: true, Events: r, Close: done, Channel: at,
					Settle: g.streamSettler(ctx, c, at, req, res.Price, requestID)}, nil
			}
			lastErr = err
		} else {
			resp, retryable, err := g.tryOnce(ctx, at, req)
			if err == nil {
				// 计量投递：usage:write + billing:settle（结算金额由 worker 按快照权威重算）
				g.notifySettled(ctx, c, at, req, res.Price, resp, requestID)
				return &Result{Resp: resp, Channel: at}, nil
			}
			lastErr = err
			if !retryable {
				break
			}
		}
	}
	releaseRes() // 全部尝试失败：释放本请求预扣（无 DB 变更，直接归还）
	return nil, finalError(lastErr)
}

// ResolveModel 模型解析（router 层；暴露便于 models 端点等复用）。
func (g *Gateway) ResolveModel(ctx context.Context, name string) (*router.ResolvedModel, error) {
	r := router.NewRouter(g.Cat)
	m, err := r.Resolve(ctx, name)
	if err != nil {
		if errors.Is(err, router.ErrModelNotFound) {
			return nil, ErrModelNotFound(name)
		}
		return nil, err
	}
	return m, nil
}

// Plan 展开降级链生成有序候选（编排层便于测试独立调用）。
func (g *Gateway) Plan(ctx context.Context, groupID int64, chain []string) ([]*router.Attempt, error) {
	r := router.NewRouter(g.Cat)
	attempts, err := r.Plan(ctx, groupID, chain)
	if err != nil {
		return nil, err
	}
	// Plan 返回 nil, nil（无可见渠道）统一转 APIError
	if len(attempts) == 0 {
		return nil, ErrNoChannel()
	}
	return attempts, nil
}

func fallbackChain(requested string, groupFb, modelFb []string) []string {
	chain := []string{requested}
	seen := map[string]bool{requested: true}
	for _, fb := range groupFb {
		if !seen[fb] {
			chain = append(chain, fb)
			seen[fb] = true
		}
	}
	for _, fb := range modelFb {
		if !seen[fb] {
			chain = append(chain, fb)
			seen[fb] = true
		}
	}
	return chain
}

// providerFor 按渠道供应商 code 取适配器；不存在视为配置错误（非重试）。
func providerFor(at *router.Attempt) (adapter.Provider, error) {
	p, ok := adapter.Get(at.Candidate.Provider.Code)
	if !ok {
		return nil, fmt.Errorf("gateway: 供应商 %s 无适配器（配置缺失）", at.Candidate.Provider.Code)
	}
	return p, nil
}

// buildRuntime 装配渠道运行时（04 §1：凭证已解密入参）。
// 信封凭证（08 §2）经主密钥解密（AAD=channel:<id> 防密文跨渠道移植）；legacy 明文透传。
// 解密失败返回明确错误（不静默空凭据发起上游请求），由编排层按渠道错误切换。
func (g *Gateway) buildRuntime(at *router.Attempt) (adapter.Runtime, error) {
	c, prov := at.Candidate.Channel, at.Candidate.Provider
	base := prov.BaseURL
	if c.BaseURL != nil && *c.BaseURL != "" {
		base = *c.BaseURL
	}
	cred := c.Credentials
	if crypto.IsSealed(cred) {
		if g == nil || g.CredMK == "" {
			return adapter.Runtime{}, fmt.Errorf("gateway: 渠道 %d 凭证为信封密文但服务未配置主密钥", c.ID)
		}
		out, err := crypto.Open(cred, g.CredMK, g.CredMKPrev, fmt.Sprintf("channel:%d", c.ID))
		if err != nil {
			return adapter.Runtime{}, fmt.Errorf("gateway: 渠道 %d 凭证解密失败（主密钥缺失/轮换/AAD 不符）: %w", c.ID, err)
		}
		cred = out
	}
	bearer := ""
	if v, ok := cred["api_key"].(string); ok {
		bearer = v
	}
	return adapter.Runtime{ID: c.ID, ProviderCode: prov.Code, BaseURL: base, Bearer: bearer}, nil
}

// newUpstreamRequest 构造上游请求。
func (g *Gateway) newUpstreamRequest(ctx context.Context, at *router.Attempt, req *ir.CanonicalRequest, requestID string) (*http.Request, error) {
	p, err := providerFor(at)
	if err != nil {
		return nil, err
	}
	rt, err := g.buildRuntime(at)
	if err != nil {
		return nil, err
	}
	return p.EncodeRequest(ctx, adapter.EncodeInput{
		Request:       req,
		UpstreamModel: at.UpstreamModel,
		Runtime:       rt,
		RequestID:     requestID,
	})
}

// tryOnce 非流尝试：成功返回解码响应；失败返回 (err, retryable)。
func (g *Gateway) tryOnce(ctx context.Context, at *router.Attempt, req *ir.CanonicalRequest) (*ir.CanonicalResponse, bool, error) {
	p, err := providerFor(at)
	if err != nil {
		return nil, false, err
	}
	httpReq, err := g.newUpstreamRequest(ctx, at, req, "")
	if err != nil {
		return nil, true, err // 编码失败可尝试其他渠道
	}
	resp, err := g.httpClient().Do(httpReq)
	if err != nil {
		return nil, true, err // 传输错误可切换
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		ue := p.NormalizeError(resp.StatusCode, resp.Header, body)
		if ue == nil {
			ue = &adapter.UpstreamError{HTTPStatus: resp.StatusCode, Code: "upstream_error", Message: "上游错误"}
		}
		return nil, ue.Retryable, ue // 可切换即重试下一渠道；RetryableOnSame（同渠道等待）在 MVP 故障转移里并入"可切换"语义
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, true, fmt.Errorf("gateway: 读取上游响应失败: %w", err)
	}
	out, err := p.DecodeResponse(body)
	if err != nil {
		return nil, false, err
	}
	return out, false, nil
}

// tryStream 流式尝试：成功返回事件流与上游体关闭函数。
func (g *Gateway) tryStream(ctx context.Context, at *router.Attempt, req *ir.CanonicalRequest) (<-chan ir.StreamEvent, func(), error) {
	p, err := providerFor(at)
	if err != nil {
		return nil, nil, err
	}
	httpReq, err := g.newUpstreamRequest(ctx, at, req, "")
	if err != nil {
		return nil, nil, err
	}
	//nolint:bodyclose // 成功路径响应体由 closeFn 交给编排层/调用方在流结束时关闭（04 §2 契约）
	resp, err := g.httpClient().Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		ue := p.NormalizeError(resp.StatusCode, resp.Header, body)
		if ue == nil {
			ue = &adapter.UpstreamError{HTTPStatus: resp.StatusCode, Code: "upstream_error", Message: "上游错误"}
		}
		return nil, nil, ue
	}
	events, err := p.DecodeStream(ctx, resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, nil, err
	}
	var once sync.Once
	closeFn := func() { once.Do(func() { _ = resp.Body.Close() }) }
	return events, closeFn, nil
}

// finalError 把最后错误归一到 APIError（对外映射）。
func finalError(err error) error {
	if err == nil {
		return ErrNoChannel()
	}
	var api *APIError
	if errors.As(err, &api) {
		return api
	}
	var ue *adapter.UpstreamError
	if errors.As(err, &ue) {
		return &APIError{Status: ue.HTTPStatus, Code: Code(ue.Code), Msg: ue.Message}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err // 客户端取消/超时按原样传播（不落成 502）
	}
	return &APIError{Status: http.StatusBadGateway, Code: CodeUpstream, Msg: "上游转发失败"}
}
