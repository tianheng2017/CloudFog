package adapter

import (
	"net/http"
	"strconv"
	"time"
)

// normalizeUpstream 按 03 §7.1 内部错误映射表归一上游 HTTP 状态。
// 上游正文解析（取可读 message）由各 Provider 完成后再套用本结果。
func normalizeUpstream(status int, retryAfterHeader string) *UpstreamError {
	retryAfter, _ := parseRetryAfter(retryAfterHeader)

	// 03 §7.1：上游状态码 → 平台错误码 / HTTP / 可重试 / 熔断
	switch status {
	case http.StatusBadRequest:
		return &UpstreamError{Code: "invalid_request", HTTPStatus: 400, Message: "上游拒绝了请求", RetryAfter: retryAfter}
	case http.StatusUnauthorized:
		return &UpstreamError{Code: "upstream_auth_failed", HTTPStatus: 502, Message: "上游鉴权失败", Retryable: true, RetryableOnSame: false, TriggersCircuit: true}
	case http.StatusForbidden:
		return &UpstreamError{Code: "permission_denied", HTTPStatus: 403, Message: "上游拒绝访问", Retryable: true, TriggersCircuit: true}
	case http.StatusNotFound:
		return &UpstreamError{Code: "model_not_found", HTTPStatus: 404, Message: "上游模型不存在", Retryable: true}
	case http.StatusRequestEntityTooLarge:
		return &UpstreamError{Code: "request_too_large", HTTPStatus: 413}
	case http.StatusTooManyRequests:
		return &UpstreamError{Code: "upstream_rate_limited", HTTPStatus: 429, Message: "上游限流", Retryable: true, RetryableOnSame: true, RetryAfter: retryAfter}
	case http.StatusInternalServerError:
		return &UpstreamError{Code: "upstream_internal_error", HTTPStatus: 502, Message: "上游内部错误", Retryable: true, TriggersCircuit: true}
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return &UpstreamError{Code: "upstream_unavailable", HTTPStatus: 503, Message: "上游不可用", Retryable: true, RetryAfter: retryAfter}
	default:
		if status == 529 { // OpenAI 过载扩展码
			return &UpstreamError{Code: "upstream_overloaded", HTTPStatus: 503, Message: "上游过载", Retryable: true, RetryAfter: retryAfter}
		}
		return &UpstreamError{Code: "upstream_error", HTTPStatus: 502, Message: "上游未知错误", Retryable: true}
	}
}

// NormalizeHTTPStatus 按 03 §7.1 映射表归一上游 HTTP 状态（子包共用导出）。
func NormalizeHTTPStatus(status int, retryAfter string) *UpstreamError {
	return normalizeUpstream(status, retryAfter)
}

// parseRetryAfter 解析 Retry-After（秒或 HTTP-date；解析失败返回 0）。
func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if sec, err := strconv.Atoi(v); err == nil && sec >= 0 {
		return time.Duration(sec) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t), true
	}
	return 0, false
}
