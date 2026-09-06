// Package billing 结算与退款领域（06 §3~§5/§10）。
// 说明：结算任务负载自包含（03 §4.5 / 06 §4.1），不依赖 usage_logs 落库顺序。
// 本包实现异步 handler 的领域编排；Redis 原子预扣与 gateway 计量钩子见同包 reserve 扩展。
package billing

import (
	"time"

	"github.com/shopspring/decimal"
)

// Tokens 一次调用的四类 token 用量（payload 传输用）。
type Tokens struct {
	Input      int `json:"input_tokens"`
	Output     int `json:"output_tokens"`
	CacheRead  int `json:"cache_read_tokens"`
	CacheWrite int `json:"cache_write_tokens"`
}

// PriceSnapshot 当次生效价固化（03 §4.2：避免调价导致账单漂移）。
type PriceSnapshot struct {
	ModelID         int64           `json:"model_id"`
	Currency        string          `json:"currency"`
	InputPer1K      decimal.Decimal `json:"input_per_1k"`
	OutputPer1K     decimal.Decimal `json:"output_per_1k"`
	CacheReadPer1K  decimal.Decimal `json:"cache_read_per_1k"`
	CacheWritePer1K decimal.Decimal `json:"cache_write_per_1k"`
	PerRequest      decimal.Decimal `json:"per_request"`
	RateMultiplier  decimal.Decimal `json:"rate_multiplier"`
}

// UsageLogPayload usage:write 任务负载（02 §6.1 usage_logs 字段子集）。
type UsageLogPayload struct {
	RequestID     string          `json:"request_id"`
	UserID        int64           `json:"user_id"`
	APIKeyID      int64           `json:"api_key_id"`
	ChannelID     int64           `json:"channel_id"`
	GroupID       *int64          `json:"group_id,omitempty"`
	Model         string          `json:"model"`
	UpstreamModel string          `json:"upstream_model"`
	ProviderCode  string          `json:"provider_code"`
	Tokens        Tokens          `json:"tokens"`
	TotalCost     decimal.Decimal `json:"total_cost"`
	PriceSnapshot PriceSnapshot   `json:"price_snapshot"`
	Stream        bool            `json:"stream"`
	StatusCode    int             `json:"status_code"`
	ErrorCode     string          `json:"error_code,omitempty"`
	DurationMs    *int            `json:"duration_ms,omitempty"`
	ClientIP      string          `json:"client_ip,omitempty"`
	UserAgent     string          `json:"user_agent,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

// SettlePayload billing:settle 任务负载（自包含）。payload 不带金额字段：
// 扣费金额由 worker 依 PriceSnapshot+Tokens 权威重算（06 §4.2），防负载被中间环节篡改金额。
type SettlePayload struct {
	RequestID     string        `json:"request_id"`
	UserID        int64         `json:"user_id"`
	APIKeyID      int64         `json:"api_key_id"`
	ChannelID     int64         `json:"channel_id"`
	GroupID       *int64        `json:"group_id,omitempty"`
	Model         string        `json:"model"`
	UpstreamModel string        `json:"upstream_model"`
	Tokens        Tokens        `json:"tokens"`
	PriceSnapshot PriceSnapshot `json:"price_snapshot"`
}

// RefundPayload billing:refund 任务负载（预扣释放/差额退回；amount >0 表示退回余额）。
type RefundPayload struct {
	RequestID string          `json:"request_id"`
	UserID    int64           `json:"user_id"`
	Amount    decimal.Decimal `json:"amount"`
	Reason    string          `json:"reason,omitempty"`
}

// ComputeCost 按价格快照与用量计算本币费用（02 §14.2 金额语义；分项高精度合计）。
func ComputeCost(p PriceSnapshot, t Tokens, perRequest bool) decimal.Decimal {
	per1k := decimal.NewFromInt(1000)
	// RateMultiplier 缺省 0 视作 1（否则会把全部费用乘成 0）
	factor := p.RateMultiplier
	if factor.IsZero() {
		factor = decimal.NewFromInt(1)
	}
	cost := p.InputPer1K.Mul(decimal.NewFromInt(int64(t.Input))).Div(per1k).
		Add(p.OutputPer1K.Mul(decimal.NewFromInt(int64(t.Output))).Div(per1k)).
		Add(p.CacheReadPer1K.Mul(decimal.NewFromInt(int64(t.CacheRead))).Div(per1k)).
		Add(p.CacheWritePer1K.Mul(decimal.NewFromInt(int64(t.CacheWrite))).Div(per1k)).
		Mul(factor)
	if perRequest {
		cost = cost.Add(p.PerRequest)
	}
	return cost
}
