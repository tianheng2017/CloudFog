// Package httpserver 承载 HTTP API 进程的服务器骨架（09 §7 健康检查端点）。
// B1 阶段仅实现 /healthz 与 /readyz；业务路由随 handler 层（后续阶段）挂载。
package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"cloudfog/internal/model"
)

// Server 包装 gin Engine + http.Server。
type Server struct {
	srv *http.Server
	eng *gin.Engine
	db  *gorm.DB // nil 时 /readyz 跳过 DB 检查（测试便利；生产必配）
	log *slog.Logger

	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
}

// Option 服务可选项（超时等；保持 New 签名向后兼容）。
type Option func(*Server)

// WithTimeouts 设置读/写/空闲超时（write=0 表示不限制，流式响应需要）。
func WithTimeouts(read, write, idle time.Duration) Option {
	return func(s *Server) {
		s.readTimeout, s.writeTimeout, s.idleTimeout = read, write, idle
	}
}

// New 构造服务（addr 形如 ":8080"，来自 config.Server.Addr）。
func New(addr string, db *gorm.DB, log *slog.Logger, opts ...Option) *Server {
	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	// 安全（08 §3.2）：IP 白名单以 ClientIP 判定——默认不信任任何代理，令
	// ClientIP=RemoteAddr，杜绝伪造 X-Forwarded-For 绕过白名单。
	// 部署在 LB/反代之后时，由部署层显式配置受信代理 CIDR（SetTrustedProxies）。
	_ = eng.SetTrustedProxies(nil)
	eng.Use(gin.Recovery(), SecurityHeaders())

	s := &Server{db: db, log: log}
	s.eng = eng

	// 09 §7：/healthz 存活（无依赖）；/readyz 就绪（PG 连通，强依赖）。
	eng.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	eng.GET("/readyz", func(c *gin.Context) {
		if s.db != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- model.Ping(s.db) }()
			select {
			case err := <-done:
				if err != nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "db"})
					return
				}
			case <-ctx.Done():
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "db-timeout"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	for _, o := range opts {
		o(s)
	}
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           eng,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       s.readTimeout,
		WriteTimeout:      s.writeTimeout, // 0 = 不限（流式 SSE 必须）
		IdleTimeout:       s.idleTimeout,
	}
	return s
}

// SecurityHeaders 基础安全响应头（08 §9；2026-09-08 补齐，此前全站零安全头）。
// API 只输出 JSON：CSP 全闭 + 禁止嵌套 + 禁 MIME 嗅探；HSTS 由 TLS 终结层（Nginx/Caddy）下发。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	}
}

// Serve 阻塞监听。正常返回 nil（含优雅关闭后 http.ErrServerClosed 归一化）。
func (s *Server) Serve() error {
	s.log.Info("http server 启动", "addr", s.srv.Addr)
	err := s.srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown 优雅关闭（ctx 控制上限，config.Server.ShutdownTimeout）。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// MountV1 挂载 v1 业务路由（cmd wiring 组装 API 后调用；healthz/readyz 不受影响）。
func (s *Server) MountV1(a *API) {
	a.Register(s.eng)
}

// MountAdmin 挂载管理端路由（07 §4：/api/v1/admin，独立鉴权链）。
func (s *Server) MountAdmin(a *Admin) {
	a.Register(s.eng)
}

// MountPortal 挂载认证与用户自助路由（07 §3：/api/v1/auth + /api/v1/me）。
func (s *Server) MountPortal(p *Portal) {
	p.Register(s.eng)
}
