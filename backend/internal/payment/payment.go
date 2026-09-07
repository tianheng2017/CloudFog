// Package payment 支付渠道抽象与订单编排（06 §7.1）。
// MVP 内置 mock 渠道（测试/开发：模拟回调演练 06 §7 注 / M11）；真实渠道
// （支付宝/微信/Stripe）在配置了商户凭据的批次按同一接口接入，接口即契约。
package payment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"
)

// Provider 支付渠道抽象（06 §7.1，新增渠道实现并注册即可）。
type Provider interface {
	Code() string // alipay | wechat | stripe | mock
	Name() string
	// CreateOrder 向渠道创建订单（余额充值等）。返回支付入口与渠道流水号占位。
	CreateOrder(ctx context.Context, in CreateOrderInput) (*OrderResult, error)
	// VerifyNotify 验签渠道异步回调；不通过返回 error（不进行任何业务处理，06 §7.3）。
	VerifyNotify(ctx context.Context, raw map[string]any) (*NotifyPayload, error)
	// CloseOrder 超时关单调用（order:close；mock 为空操作）。
	CloseOrder(ctx context.Context, orderNo string) error
}

type CreateOrderInput struct {
	OrderNo   string
	Amount    decimal.Decimal
	Currency  string
	Subject   string
	ExpireIn  time.Duration
	ReturnURL string
	NotifyURL string
}

type OrderResult struct {
	TradeNo  string // 渠道预生成流水号占位（mock 下单即返回；真实渠道可能为空待支付后回填）
	PayURL   string // 跳转/扫码支付链接
	ExpireAt time.Time
}

// NotifyPayload 规范化回调载荷（06 §7.1）。幂等键 = ProviderTradeNo。
type NotifyPayload struct {
	ProviderTradeNo string
	OrderNo         string
	Amount          decimal.Decimal
	Currency        string
	PaidAt          time.Time
}

var (
	ErrInvalidNotify = errors.New("payment: 回调验签失败")
	ErrUnsupported   = errors.New("payment: 不支持的支付渠道")
)

// Service 渠道注册表（下单/回调分发）。内置 mock（未配置真实凭据时兜底，仅限开发/测试）。
type Service struct {
	log         *slog.Logger
	byCode      map[string]Provider
	defaultCode string
}

// NewService 构建渠道服务。providers 为已启用真实渠道（凭据配置），否则仅 mock 并在日志标注。
func NewService(log *slog.Logger, real map[string]Provider) *Service {
	s := &Service{byCode: map[string]Provider{}, log: log}
	if s.log == nil {
		s.log = slog.Default()
	}
	if len(real) == 0 {
		m := &MockProvider{}
		s.byCode[m.Code()] = m
		s.defaultCode = m.Code()
		s.log.Warn("payment: 未配置真实支付渠道凭据，启用内置模拟渠道（仅限开发/测试，M11 演练用）")
		return s
	}
	for c, p := range real {
		s.byCode[c] = p
		if s.defaultCode == "" {
			s.defaultCode = c
		}
	}
	return s
}

// Provider 按 code 取渠道。
func (s *Service) Provider(code string) (Provider, error) {
	if p, ok := s.byCode[code]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupported, code)
}

// DefaultProvider 默认渠道（创建订单未指定 provider_code 时用）。
func (s *Service) DefaultProvider() Provider { return s.byCode[s.defaultCode] }

// List 已启用渠道清单（GET /payment/providers）。
func (s *Service) List() []Provider {
	out := make([]Provider, 0, len(s.byCode))
	for _, p := range s.byCode {
		out = append(out, p)
	}
	return out
}

// MockProvider 模拟支付渠道（06 §7 注：测试渠道模拟回调演练；签名=回调字段+shared secret）。
type MockProvider struct {
	// Secret 回调签名密钥。空时接受任意回调（测试直连）；非空时校验 sign 字段。
	Secret string
}

func (m *MockProvider) Code() string { return "mock" }
func (m *MockProvider) Name() string { return "模拟支付（测试）" }

func (m *MockProvider) CreateOrder(_ context.Context, in CreateOrderInput) (*OrderResult, error) {
	if in.Amount.Sign() <= 0 {
		return nil, errors.New("payment: 金额必须为正")
	}
	return &OrderResult{
		TradeNo:  "mock-" + in.OrderNo,
		PayURL:   "mock://pay/" + in.OrderNo + "?amount=" + in.Amount.String(),
		ExpireAt: time.Now().UTC().Add(in.ExpireIn),
	}, nil
}

// VerifyNotify mock 验签：默认（Secret 空）接受 mock 直连回调；配置 Secret 时校验 sign=
// sha256(order_no|amount|provider_trade_no + secret) 前 16 位。
func (m *MockProvider) VerifyNotify(_ context.Context, raw map[string]any) (*NotifyPayload, error) {
	str := func(k string) string {
		if v, ok := raw[k].(string); ok {
			return v
		}
		return ""
	}
	amountStr := str("amount")
	amount, err := decimal.NewFromString(amountStr)
	if err != nil || amount.Sign() <= 0 {
		return nil, fmt.Errorf("%w: amount 非法", ErrInvalidNotify)
	}
	orderNo := str("order_no")
	tradeNo := str("provider_trade_no")
	if orderNo == "" || tradeNo == "" {
		return nil, fmt.Errorf("%w: order_no/provider_trade_no 缺失", ErrInvalidNotify)
	}
	if m.Secret != "" {
		expect := mockSign(m.Secret, orderNo, tradeNo, amountStr)
		if str("sign") != expect {
			return nil, fmt.Errorf("%w: 签名不符", ErrInvalidNotify)
		}
	}
	paidAt := time.Now().UTC()
	if ts, ok := raw["paid_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			paidAt = t
		}
	}
	return &NotifyPayload{ProviderTradeNo: tradeNo, OrderNo: orderNo, Amount: amount, Currency: "USD", PaidAt: paidAt}, nil
}

func (m *MockProvider) CloseOrder(_ context.Context, _ string) error { return nil }

// mockSign mock 渠道回调签名：sha256(secret||order_no|provider_trade_no|amount) 前 8 字节 hex。
// 仅测试语义（防同网段伪造），真实渠道用各自 SDK 验签。
func mockSign(secret, orderNo, tradeNo, amount string) string {
	h := sha256.Sum256([]byte(secret + "|" + orderNo + "|" + tradeNo + "|" + amount))
	return hex.EncodeToString(h[:8])
}

// NewOrderNo 平台订单号：R + yymmdd + 8 字节随机 hex（入库唯一，撞唯一由调用方重试）。
func NewOrderNo(now time.Time) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "R" + now.UTC().Format("060102") + hex.EncodeToString(buf)
}
