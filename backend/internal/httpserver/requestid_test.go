package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestIDConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, requestID(c))
	})
	r.GET("/abort", func(c *gin.Context) {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "boom")
	})

	t.Run("生成值与响应头/错误体同源", func(t *testing.T) {
		// 成功响应：header 存在且与 body 一致
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ok", nil)
		r.ServeHTTP(w, req)
		hdr := w.Header().Get(requestIDHeader)
		if hdr == "" || hdr != w.Body.String() {
			t.Fatalf("成功响应 header/body 应同值: hdr=%q body=%q", hdr, w.Body.String())
		}
		// 错误响应：header 与错误体 request_id 一致（曾各生成一次导致不同）
		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/abort", nil)
		r.ServeHTTP(w2, req2)
		hdr2 := w2.Header().Get(requestIDHeader)
		var body struct {
			Error struct {
				RequestID string `json:"request_id"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w2.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if hdr2 == "" || hdr2 != body.Error.RequestID {
			t.Fatalf("错误响应 header/body 应同值: hdr=%q body=%q", hdr2, body.Error.RequestID)
		}
	})

	t.Run("透传客户端头", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ok", nil)
		req.Header.Set(requestIDHeader, "client-trace-123")
		r.ServeHTTP(w, req)
		if got := w.Header().Get(requestIDHeader); got != "client-trace-123" {
			t.Fatalf("应透传客户端 id, got %q", got)
		}
	})
}
