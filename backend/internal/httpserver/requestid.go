package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDKey = "cloudfog.request_id"

// requestIDHeader 透传/回写用的响应头名（03 §2 中间件链第 1 步）。
const requestIDHeader = "X-Request-ID"

// maxRequestIDLen 透传头长度上限（防客户端超长头滥用/日志膨胀）。
const maxRequestIDLen = 128

// RequestIDMiddleware 中间件链第 1 步（03 §2）：生成/透传 request_id 并回写响应头。
// 全链（业务/错误体/结算幂等键/Redis 审计键）共用同一值，保证对账可追踪。
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.GetHeader(requestIDHeader))
		if id == "" {
			id = genRequestID()
		}
		if len(id) > maxRequestIDLen {
			id = id[:maxRequestIDLen]
		}
		c.Set(requestIDKey, id)
		c.Header(requestIDHeader, id) // 成功/失败响应均回写，客户端可据此关联请求
		c.Next()
	}
}

// requestID 取当前请求 id：优先上下文（中间件已生成/透传）；未挂中间件时兜底生成
// （存量测试直接挂 APIKeyAuth 的场景），并同步上下文与响应头保证一致性。
func requestID(c *gin.Context) string {
	if id, ok := c.Get(requestIDKey); ok {
		if s, ok := id.(string); ok && s != "" {
			return s
		}
	}
	id := genRequestID()
	c.Set(requestIDKey, id)
	c.Header(requestIDHeader, id)
	return id
}

// genRequestID req_<unixnano>-<8hex>（时间唯一 + 随机后缀防并发预测）。
func genRequestID() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("req_%d-%s", time.Now().UnixNano(), hex.EncodeToString(buf))
}
