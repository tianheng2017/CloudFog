package httpserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"cloudfog/internal/model"
	"cloudfog/internal/payment"
	"cloudfog/internal/repository"
)

const (
	// rechargeMaxAmount 单笔充值上限（防异常大额；风控后续按 06 §8 扩展监控）。
	rechargeMaxAmount  = "5000"
	defaultOrderExpire = 30 * time.Minute
)

// handlePaymentProviders 可支付渠道列表（07 §3.1 GET /payment/providers）。
func (p *Portal) handlePaymentProviders(c *gin.Context) {
	if _, ok := currentUser(c); !ok {
		return
	}
	provs := p.Pay.List()
	items := make([]gin.H, 0, len(provs))
	for _, pr := range provs {
		items = append(items, gin.H{"code": pr.Code(), "name": pr.Name()})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// handlePaymentOrderCreate 创建充值订单（07 §3.1 POST /payment/orders）。
// MVP：充值（type=recharge），经渠道 CreateOrder 拿支付入口，状态 pending。
func (p *Portal) handlePaymentOrderCreate(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	var req struct {
		Amount       string `json:"amount"`
		Currency     string `json:"currency"`
		ProviderCode string `json:"provider_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "amount 须为合法十进制")
		return
	}
	capAmt, _ := decimal.NewFromString(rechargeMaxAmount)
	if amount.Sign() <= 0 || amount.GreaterThan(capAmt) {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "amount 须在 (0, 5000] 区间")
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "USD"
	}
	if currency != "USD" {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "MVP 仅支持 USD 充值")
		return
	}
	expire := p.OrderExpire
	if expire <= 0 {
		expire = defaultOrderExpire
	}
	now := time.Now().UTC()
	ctx := c.Request.Context()
	var order *model.PaymentOrder
	var payURL string
	// order_no 撞唯一重试（≤3 次；随机熵下基本不发生）
	for attempt := 0; attempt < 3; attempt++ {
		prov, perr := p.pickProvider(req.ProviderCode)
		if perr != nil {
			writeAPIError(c, http.StatusBadRequest, "invalid_request", perr.Error())
			return
		}
		no := payment.NewOrderNo(now)
		res, cerr := prov.CreateOrder(ctx, payment.CreateOrderInput{
			OrderNo: no, Amount: amount, Currency: currency,
			Subject: "CloudFog 余额充值", ExpireIn: expire,
		})
		if cerr != nil {
			p.log().Error("portal pay create order(provider)", "error", cerr)
			writeAPIError(c, http.StatusBadGateway, "payment_unavailable", "支付渠道暂不可用")
			return
		}
		exp := res.ExpireAt
		if exp.IsZero() {
			exp = now.Add(expire)
		}
		order = &model.PaymentOrder{
			OrderNo: no, UserID: u.ID, ProviderCode: prov.Code(),
			ProviderTradeNo: res.TradeNo,
			Amount:          model.Decimal{Decimal: amount},
			Currency:        currency,
			ExchangeRate:    model.Decimal{Decimal: decimal.NewFromInt(1)},
			Type:            "recharge", Status: "pending",
			ExpiredAt: &exp,
		}
		payURL = res.PayURL
		if oerr := p.Repo.CreatePaymentOrder(ctx, order); oerr != nil {
			if repository.IsUniqueViolation(oerr) {
				continue // order_no 碰撞，重生成重试
			}
			p.log().Error("portal pay create order", "error", oerr)
			writeAPIError(c, http.StatusInternalServerError, "server_error", "下单失败")
			return
		}
		break
	}
	if order == nil || order.ID == 0 {
		writeAPIError(c, http.StatusInternalServerError, "server_error", "下单失败，请重试")
		return
	}
	p.log().Info("portal pay order created", "user_id", u.ID, "order_no", order.OrderNo, "amount", amount.String())
	c.JSON(http.StatusOK, gin.H{
		"order_no": order.OrderNo, "amount": amount.String(), "currency": currency,
		"provider": order.ProviderCode, "status": "pending",
		"pay_url": payURL, "expires_at": order.ExpiredAt,
	})
}

// handlePaymentOrderGet 查询自己的订单（07 §3.1 GET /payment/orders/{no}）。
func (p *Portal) handlePaymentOrderGet(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	no := strings.TrimSpace(c.Param("no"))
	if no == "" {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "缺少订单号")
		return
	}
	o, err := p.Repo.PaymentOrderByNo(c.Request.Context(), no)
	if err != nil {
		p.log().Error("portal pay order get", "order_no", no, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	if o == nil || o.UserID != u.ID {
		writeAPIError(c, http.StatusNotFound, "not_found", "订单不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"order_no": o.OrderNo, "amount": o.Amount.String(), "currency": o.Currency,
		"credited_amount": o.CreditedAmount.String(), "provider": o.ProviderCode,
		"type": o.Type, "status": o.Status,
		"paid_at": o.PaidAt, "created_at": o.CreatedAt, "expires_at": o.ExpiredAt,
	})
}

// pickProvider 创建订单渠道选择：显式 code 或默认；不存在 → 错误（400 由调用方映射）。
func (p *Portal) pickProvider(code string) (payment.Provider, error) {
	if code != "" {
		return p.Pay.Provider(code)
	}
	return p.Pay.DefaultProvider(), nil
}
