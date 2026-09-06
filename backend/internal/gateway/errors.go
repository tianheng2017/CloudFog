package gateway

import (
	"fmt"
	"net/http"
)

// Code 平台对外错误码（07 §5.2 / 03 §7.1 内部表取交集，API 层输出）。
type Code string

const (
	CodeInvalidRequest      Code = "invalid_request"
	CodeModelNotFound       Code = "model_not_found"
	CodeModelNotAllowed     Code = "permission_denied" // 模型白名单拒绝（中间件链第 8 步）
	CodeInsufficientBalance Code = "insufficient_balance"
	CodeNoChannel           Code = "no_available_channel"
	CodeUpstream            Code = "upstream_error"
)

// APIError 网关层决策错误（预检/路由/无候选），携带 HTTP 状态与对外码。
type APIError struct {
	Status int
	Code   Code
	Msg    string
}

func (e *APIError) Error() string { return fmt.Sprintf("[%d] %s: %s", e.Status, e.Code, e.Msg) }

func errStatus(status int, code Code, msg string) *APIError {
	return &APIError{Status: status, Code: code, Msg: msg}
}

// ErrInvalidRequest 请求体/参数归一化失败（400）。
func ErrInvalidRequest(msg string) *APIError {
	return errStatus(http.StatusBadRequest, CodeInvalidRequest, msg)
}

// ErrModelNotFound 请求模型不在目录/不可用（404，07 §5.2）。
func ErrModelNotFound(model string) *APIError {
	return errStatus(http.StatusNotFound, CodeModelNotFound, fmt.Sprintf("模型 %s 不存在或不可用", model))
}

// ErrModelNotAllowed 密钥/分组白名单拒绝（403）。
func ErrModelNotAllowed(model string) *APIError {
	return errStatus(http.StatusForbidden, CodeModelNotAllowed, fmt.Sprintf("无权使用模型 %s", model))
}

// ErrInsufficientBalance 余额不足（402，03 §4.3 / M5）。
func ErrInsufficientBalance() *APIError {
	return errStatus(http.StatusPaymentRequired, CodeInsufficientBalance, "余额不足")
}

// ErrNoChannel 无可用渠道（503）。
func ErrNoChannel() *APIError {
	return errStatus(http.StatusServiceUnavailable, CodeNoChannel, "当前无可用渠道，请稍后重试")
}

// ErrBillingUnavailable 结算投递不可用（503，fail-closed：不提供可能漏计费的服务）。
func ErrBillingUnavailable() *APIError {
	return errStatus(http.StatusServiceUnavailable, "billing_unavailable", "结算通道暂不可用，请稍后重试")
}
