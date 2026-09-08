package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// 09 §7：/healthz 存活（无依赖）恒 200；db 为 nil 时 /readyz 也 200（测试便利分支）。
func TestHealthzReadyz(t *testing.T) {
	s := New(":0", nil, discardLog())
	handler := s.srv.Handler

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1"+path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200 (body=%s)", path, w.Code, w.Body.String())
		}
	}
}

// 安全响应头（08 §9）：全站必须带 nosniff/DENY/CSP 等基础头。
func TestSecurityHeaders(t *testing.T) {
	s := New(":0", nil, discardLog())
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1/healthz", nil)
	w := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, req)

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := w.Header().Get(k); got != v {
			t.Fatalf("响应头 %s = %q, want %q", k, got, v)
		}
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp == "" {
		t.Fatal("缺少 Content-Security-Policy（API 应全闭）")
	}
}

// 业务路由未挂载前：未知路径应 404（gin 默认）。
func TestUnknownPath404(t *testing.T) {
	s := New(":0", nil, discardLog())
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1/api/v1/none", nil)
	w := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知路径 = %d, want 404", w.Code)
	}
}
