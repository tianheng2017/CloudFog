package httpserver

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"cloudfog/internal/auth"
)

// principalCtxKey gin 上下文存放 *auth.Principal。
const principalCtxKey = "cloudfog.auth.principal"

// APIKeyAuth 鉴权中间件（08 §3.2；中间件链第 4 步的 PG 直查实现）。
// 失败响应：凭证无效 → 401 invalid_api_key；IP 白名单拒绝 → 403 ip_not_allowed
// （03 §7.2 OpenAI 风格错误体）。成功把 Principal 注入 gin 上下文（PrincipalOf 取用）。
func APIKeyAuth(store auth.Lookup, salt string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tok, ok := auth.BearerToken(c.GetHeader("Authorization"))
		if !ok {
			abortAuth(c, http.StatusUnauthorized, auth.CodeInvalidAPIKey, "缺少 Bearer API key")
			return
		}
		p, err := auth.Authenticate(c.Request.Context(), store, salt, tok, c.ClientIP())
		if err != nil {
			var ae *auth.Error
			if errors.As(err, &ae) {
				status := http.StatusUnauthorized
				if ae.Code == auth.CodeIPNotAllowed {
					status = http.StatusForbidden
				}
				abortAuth(c, status, ae.Code, ae.Message)
				return
			}
			// 基础设施错误（DB 不可用等）：不泄露细节，按 503 处理让客户端重试
			abortAuth(c, http.StatusServiceUnavailable, auth.CodeInvalidAPIKey, "鉴权服务暂不可用")
			return
		}
		c.Set(principalCtxKey, p)
		c.Next()
	}
}

// PrincipalOf 取当前请求鉴权主体；未鉴权返回 (nil, false)。
func PrincipalOf(c *gin.Context) (*auth.Principal, bool) {
	p, ok := c.Get(principalCtxKey)
	if !ok {
		return nil, false
	}
	pr, ok := p.(*auth.Principal)
	return pr, ok
}

// abortAuth 以 OpenAI 兼容错误体终止请求（03 §7.2）。request_id 与 RequestID 中间件同值。
func abortAuth(c *gin.Context, status int, code auth.Code, message string) {
	id := requestID(c)
	c.Header(requestIDHeader, id)
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"message":    message,
			"type":       "cloudfog_error",
			"code":       string(code),
			"param":      nil,
			"request_id": id,
		},
	})
}
