package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"cloudfog/internal/payment"
)

// handlePaymentNotify 支付渠道异步回调（06 §7.3，无需会话，渠道服务器直连）。
// 顺序：验签（失败直接 400 不做业务）→ 定位订单 → 金额校验 → 投递 payment:confirm（critical）
// → 快速响应 success；重复回调（订单已 paid 同流水号）直接 success（幂等语义）。
func (p *Portal) handlePaymentNotify(c *gin.Context) {
	code := c.Param("provider")
	prov, err := p.Pay.Provider(code)
	if err != nil {
		writeAPIError(c, http.StatusNotFound, "not_found", "支付渠道不存在")
		return
	}
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil || raw == nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "回调体非法")
		return
	}
	payload, err := prov.VerifyNotify(c.Request.Context(), raw)
	if err != nil {
		// 验签失败：不进行任何业务处理（06 §7.3）；渠道会重发，日志留痕排查
		p.log().Warn("payment notify verify failed", "provider", code, "error", err)
		writeAPIError(c, http.StatusBadRequest, "invalid_notify", "回调验签失败")
		return
	}
	ctx := c.Request.Context()
	o, err := p.Repo.PaymentOrderByNo(ctx, payload.OrderNo)
	if err != nil {
		p.log().Error("payment notify order lookup", "order_no", payload.OrderNo, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	if o == nil {
		writeAPIError(c, http.StatusNotFound, "not_found", "订单不存在")
		return
	}
	// 重复回调（已入账/处理中）→ 直接成功（幂等）；渠道侧会持续重发直到收到 success
	if o.Status == "paid" {
		if o.ProviderTradeNo != payload.ProviderTradeNo {
			// 已入账但回调流水号不同：异常（串号/重放他单/渠道异常），不得再入账，
			// 记 error 供对账排查；仍返回 success 避免渠道无限重发。
			p.log().Error("payment notify mismatch trade for paid order", "order_no", o.OrderNo,
				"existing", o.ProviderTradeNo, "got", payload.ProviderTradeNo)
		}
		c.JSON(http.StatusOK, gin.H{"result": "success", "duplicate": true})
		return
	}
	if o.ProviderTradeNo != "" && o.ProviderTradeNo == payload.ProviderTradeNo {
		c.JSON(http.StatusOK, gin.H{"result": "success", "duplicate": true})
		return
	}
	// 金额校验：必须等于订单金额（容差 0；防篡改 06 §7.3）。异常需告警排查
	if !payload.Amount.Equal(o.Amount.Decimal) {
		p.log().Error("payment notify amount mismatch", "order_no", o.OrderNo,
			"expected", o.Amount.String(), "got", payload.Amount.String())
		writeAPIError(c, http.StatusBadRequest, "amount_mismatch", "回调金额与订单不一致")
		return
	}
	if p.PayEnq == nil {
		writeAPIError(c, http.StatusServiceUnavailable, "payment_unavailable", "结算服务暂不可用")
		return
	}
	prod := &payment.Producer{Enq: p.PayEnq}
	if err := prod.SendConfirm(ctx, &payment.ConfirmPayload{
		OrderNo: payload.OrderNo, ProviderTradeNo: payload.ProviderTradeNo,
		PaidAt: payload.PaidAt, RawNotify: raw,
	}); err != nil {
		p.log().Error("payment notify enqueue", "order_no", o.OrderNo, "error", err)
		writeAPIError(c, http.StatusServiceUnavailable, "payment_unavailable", "回调处理暂不可用")
		return
	}
	p.log().Info("payment notify accepted", "order_no", o.OrderNo,
		"provider_trade_no", payload.ProviderTradeNo, "amount", payload.Amount.String())
	c.JSON(http.StatusOK, gin.H{"result": "success"})
}
