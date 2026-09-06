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
	"net/http"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"cloudfog/internal/billing"
	"cloudfog/internal/ir"
	"cloudfog/internal/model"
	"cloudfog/internal/pkg/adapter"
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
	// 时不得放行非流计费请求，否则预扣永不释放/漏计费（b2-7 拉通审查发现）。
	if !req.Stream && g.Res != nil && g.Prod == nil {
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
				return &Result{Stream: true, Events: r, Close: done, Channel: at}, nil
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
// 说明：MVP 阶段直接读取 channel.credentials.api_key（dev/测试用明文）；生产信封解密随凭证管理模块接入（08 §2）。
func buildRuntime(at *router.Attempt) adapter.Runtime {
	c, prov := at.Candidate.Channel, at.Candidate.Provider
	base := prov.BaseURL
	if c.BaseURL != nil && *c.BaseURL != "" {
		base = *c.BaseURL
	}
	bearer := ""
	if v, ok := c.Credentials["api_key"].(string); ok {
		bearer = v
	}
	return adapter.Runtime{ID: c.ID, ProviderCode: prov.Code, BaseURL: base, Bearer: bearer}
}

// newUpstreamRequest 构造上游请求。
func newUpstreamRequest(ctx context.Context, at *router.Attempt, req *ir.CanonicalRequest, requestID string) (*http.Request, error) {
	p, err := providerFor(at)
	if err != nil {
		return nil, err
	}
	return p.EncodeRequest(ctx, adapter.EncodeInput{
		Request:       req,
		UpstreamModel: at.UpstreamModel,
		Runtime:       buildRuntime(at),
		RequestID:     requestID,
	})
}

// tryOnce 非流尝试：成功返回解码响应；失败返回 (err, retryable)。
func (g *Gateway) tryOnce(ctx context.Context, at *router.Attempt, req *ir.CanonicalRequest) (*ir.CanonicalResponse, bool, error) {
	p, err := providerFor(at)
	if err != nil {
		return nil, false, err
	}
	httpReq, err := newUpstreamRequest(ctx, at, req, "")
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
	httpReq, err := newUpstreamRequest(ctx, at, req, "")
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
