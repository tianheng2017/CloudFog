package payment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// TestNoAutoMock 安全不变量（审计 R1）：无真实凭据时不得自动注册 mock——
// 否则无签名 notify 暴露到真实环境可被伪造回调充值。
func TestNoAutoMock(t *testing.T) {
	s := NewService(nil, nil)
	if len(s.List()) != 0 {
		t.Fatalf("未配置渠道时注册表应为空（防自动 mock），got %d", len(s.List()))
	}
	if s.DefaultProvider() != nil {
		t.Fatal("空注册表默认渠道应为 nil")
	}
	if _, err := s.Provider("mock"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("空注册表不应存在 mock, err=%v", err)
	}
	// 显式注册后可用（部署层显式注入语义）
	s.RegisterProvider(&MockProvider{})
	if len(s.List()) != 1 || s.DefaultProvider().Code() != "mock" {
		t.Fatal("显式注册 mock 后应可用")
	}
}

func TestMockProviderCreateAndVerify(t *testing.T) {
	m := &MockProvider{Secret: "test-secret"}
	res, err := m.CreateOrder(context.Background(), CreateOrderInput{
		OrderNo: "R260907ABC", Amount: decimal.NewFromFloat(12.34), Currency: "USD",
		ExpireIn: 30 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.PayURL, "R260907ABC") {
		t.Fatalf("pay_url 应含订单号: %s", res.PayURL)
	}
	// 无签名字段 → 验签失败
	raw := map[string]any{"order_no": "R260907ABC", "provider_trade_no": "mock-R260907ABC", "amount": "12.34"}
	if _, err := m.VerifyNotify(context.Background(), raw); err == nil {
		t.Fatal("配置 Secret 后无签名回调应拒绝")
	}
	// 正确签名 → 通过
	raw["sign"] = mockSign("test-secret", "R260907ABC", "mock-R260907ABC", "12.34")
	p, err := m.VerifyNotify(context.Background(), raw)
	if err != nil {
		t.Fatalf("正确签名应通过: %v", err)
	}
	if p.OrderNo != "R260907ABC" || !p.Amount.Equal(decimal.NewFromFloat(12.34)) {
		t.Fatalf("回调载荷异常: %+v", p)
	}
	// 金额被篡改 → 拒绝
	raw["amount"] = "9999"
	if _, err := m.VerifyNotify(context.Background(), raw); err == nil {
		t.Fatal("篡改金额应拒绝")
	}
}
