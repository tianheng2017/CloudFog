package httpserver

import (
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
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200 (body=%s)", path, w.Code, w.Body.String())
		}
	}
}

// 业务路由未挂载前：未知路径应 404（gin 默认）。
func TestUnknownPath404(t *testing.T) {
	s := New(":0", nil, discardLog())
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/none", nil)
	w := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知路径 = %d, want 404", w.Code)
	}
}
