package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"cloudfog/internal/auth"
	"cloudfog/internal/model"
	"cloudfog/internal/payment"
	"cloudfog/internal/pkg/password"
	"cloudfog/internal/repository"
	"cloudfog/internal/task"
)

// Portal 用户自助/认证端（07 §3：/api/v1/auth + /api/v1/me + /api/v1/payment）。
// MVP 载体：opaque 会话 token（sess_ 前缀，Bearer/Cookie 双通道），与 API Key(sk-cf-)命名空间隔离。
type Portal struct {
	Repo *repository.Repository
	Salt string
	Log  *slog.Logger
	// Pay 支付渠道服务（b3-4；nil 时 payment 端点返回不可用）。
	Pay *payment.Service
	// PayEnq 回调 → payment:confirm 投递器（nil 时 notify 返回 503，broker 不可达 fail-closed）。
	PayEnq task.TaskEnqueuer
	// OrderExpire 充值订单有效期（<=0 用默认 30m）。
	OrderExpire time.Duration

	// RegistrationEnabled 注册总开关。config registration_enabled 接线前默认开启
	//（10 §3.2 文档键已规划；B4 系统配置落库时改读配置）。
	RegistrationEnabled bool

	mu    sync.Mutex
	fails map[string]*failBucket // 登录失败锁定（进程内；多副本部署时改 Redis 计数）
}

type failBucket struct {
	count int
	until time.Time
}

const (
	sessionTTL        = 24 * time.Hour
	sessionCookieName = "cloudfog_session"
	sessionHashCtxKey = "cloudfog.session.hash"
	maxLoginFailures  = 5
	loginLockWindow   = 15 * time.Minute
	sessionTouchEvery = time.Minute
	// loginFailMaxTracked 失败计数 map 上限：超过触发 prune 清掉已过期/未锁定条目。
	// 无界增长是 DoS 面——攻击者枚举任意不存在账号也会落条目（防枚举 401 与成功同文案）。
	loginFailMaxTracked = 4096
)

func (p *Portal) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

func (p *Portal) ensureFails() map[string]*failBucket {
	if p.fails == nil {
		p.fails = map[string]*failBucket{}
	}
	return p.fails
}

// Register 挂载认证（免会话）与自助（会话鉴权）路由。
func (p *Portal) Register(eng *gin.Engine) {
	authGrp := eng.Group("/api/v1/auth")
	authGrp.Use(RequestIDMiddleware())
	authGrp.POST("/register", p.handleRegister)
	authGrp.POST("/login", p.handleLogin)
	authGrp.POST("/logout", p.sessionAuth(), p.handleLogout)

	// 自助 /api/v1/me/*：会话鉴权组级（b33-3/b33-4）
	me := eng.Group("/api/v1/me")
	me.Use(RequestIDMiddleware(), p.sessionAuth())
	me.GET("", p.handleMe)
	me.GET("/balance", p.handleMeBalance)
	me.GET("/groups", p.handleMeGroups)
	me.GET("/keys", p.handleMyKeysList)
	me.POST("/keys", p.handleMyKeysCreate)
	me.PATCH("/keys/:id", p.handleMyKeysPatch)
	me.DELETE("/keys/:id", p.handleMyKeysDelete)

	// 充值支付（b3-4，07 §3.1 /api/v1/payment，会话鉴权）
	payGrp := eng.Group("/api/v1/payment")
	payGrp.Use(RequestIDMiddleware(), p.sessionAuth())
	payGrp.GET("/providers", p.handlePaymentProviders)
	payGrp.POST("/orders", p.handlePaymentOrderCreate)
	payGrp.GET("/orders/:no", p.handlePaymentOrderGet)

	// 支付渠道回调（06 §7.3）：无需会话，渠道服务器直连；验签/金额/幂等在 handler 层
	notify := eng.Group("/api/v1/payment/notify")
	notify.Use(RequestIDMiddleware())
	notify.POST("/:provider", p.handlePaymentNotify)
}

func (p *Portal) blocked(login string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.ensureFails()[strings.ToLower(login)]
	if b == nil || b.until.IsZero() || time.Now().After(b.until) {
		return time.Time{}, false
	}
	return b.until, true
}

func (p *Portal) recordFailure(login string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := strings.ToLower(login)
	f := p.ensureFails()[key]
	now := time.Now()
	if f == nil {
		f = &failBucket{}
		p.fails[key] = f
	} else if !f.until.IsZero() && now.After(f.until) {
		// 上次锁定窗口已过 → 重新累积（until=zero 表示未锁定，勿按过期重置计数）
		f.count = 0
		f.until = time.Time{}
	}
	f.count++
	if f.count >= maxLoginFailures {
		f.until = now.Add(loginLockWindow)
	}
	// 有界：超过阈值时清扫已失效条目（未锁定计数无跨窗口价值；锁定已过窗口的可重计）
	if len(p.fails) > loginFailMaxTracked {
		p.pruneLocked(now)
	}
}

// pruneLocked 删除过期/未锁定条目（须持锁调用）。保留仍生效中的锁定。
func (p *Portal) pruneLocked(now time.Time) {
	for k, b := range p.fails {
		if b.until.IsZero() || now.After(b.until) {
			delete(p.fails, k)
		}
	}
}

func (p *Portal) clearFailures(login string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.ensureFails(), strings.ToLower(login))
}

// sessionToken 生成会话 token（sess_ + 24B 随机）。仅下发一次；库中只存 SHA-256(salt||token)。
func sessionToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "sess_" + hex.EncodeToString(buf), nil
}

// sessionAuth 会话鉴权中间件（07 §3.0）。接受 Bearer sess_* 或 HttpOnly Cookie。
// 成功注入 Principal（User）与会话 hash 到 gin 上下文。
func (p *Portal) sessionAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tok, ok := auth.BearerToken(c.GetHeader("Authorization"))
		if !ok {
			tok, _ = c.Cookie(sessionCookieName)
		}
		if tok == "" || !strings.HasPrefix(tok, "sess_") {
			abortAuth(c, http.StatusUnauthorized, auth.CodeSessionExpired, "缺少或无效的会话")
			return
		}
		hash := auth.HashKey(p.Salt, tok)
		s, err := p.Repo.AuthSessionByTokenHash(c.Request.Context(), hash)
		if err != nil {
			p.log().Error("portal session lookup", "error", err)
			abortAuth(c, http.StatusServiceUnavailable, auth.CodeSessionExpired, "鉴权服务暂不可用")
			return
		}
		if s == nil {
			abortAuth(c, http.StatusUnauthorized, auth.CodeSessionExpired, "会话不存在或已登出")
			return
		}
		now := time.Now().UTC()
		if now.After(s.ExpiresAt) {
			_ = p.Repo.DeleteAuthSessionByHash(c.Request.Context(), hash)
			abortAuth(c, http.StatusUnauthorized, auth.CodeSessionExpired, "会话已过期")
			return
		}
		u, err := p.Repo.UserByID(c.Request.Context(), s.UserID)
		if err != nil {
			p.log().Error("portal session user", "error", err)
			abortAuth(c, http.StatusServiceUnavailable, auth.CodeSessionExpired, "鉴权服务暂不可用")
			return
		}
		if u == nil || u.Status != "active" {
			abortAuth(c, http.StatusUnauthorized, auth.CodeSessionExpired, "账号不可用")
			return
		}
		c.Set(principalCtxKey, &auth.Principal{User: u})
		c.Set(sessionHashCtxKey, hash)
		if now.Sub(s.LastActiveAt) > sessionTouchEvery {
			_ = p.Repo.TouchAuthSession(c.Request.Context(), s.ID, now)
		}
		c.Next()
	}
}

// writePortalUnauthorized 401 通用（登录失败统一文案防账号枚举）。
func writePortalUnauthorized(c *gin.Context, msg string) {
	abortAuth(c, http.StatusUnauthorized, auth.CodeInvalidAPIKey, msg)
}

func (p *Portal) handleRegister(c *gin.Context) {
	if !p.RegistrationEnabled {
		writeAPIError(c, http.StatusForbidden, "registration_closed", "注册已关闭")
		return
	}
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	// 统一小写存储：登录按 lower(email)/lower(username) 比对，大小写变体并存会产生登录歧义（First 取一）
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Username == "" || req.Email == "" || !strings.Contains(req.Email, "@") || req.Password == "" {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "username/email/password 必填且 email 须含 @")
		return
	}
	if len(req.Password) < 8 {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "密码至少 8 位")
		return
	}
	ctx := c.Request.Context()
	g, err := p.Repo.FirstActiveGroup(ctx)
	if err != nil {
		p.log().Error("portal register group", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "注册暂不可用")
		return
	}
	if g == nil {
		writeAPIError(c, http.StatusServiceUnavailable, "registration_unavailable", "系统暂未配置可用分组")
		return
	}
	hash, err := password.Hash(req.Password)
	if err != nil {
		p.log().Error("portal register hash", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "注册失败")
		return
	}
	u := &model.User{Username: req.Username, Email: req.Email, PasswordHash: &hash,
		PasswordAlgo: "argon2id", Role: "user", Status: "active", DefaultGroupID: &g.ID, Timezone: "UTC"}
	if err := p.Repo.CreateUser(ctx, u); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAPIError(c, http.StatusConflict, "conflict", "用户名或邮箱已被注册")
			return
		}
		p.log().Error("portal register create", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "注册失败")
		return
	}
	// 默认分组自动进可见集合（Key 创建/切换分组数据源）
	_ = p.Repo.AllowUserGroup(ctx, u.ID, g.ID)
	p.log().Info("portal register", "user_id", u.ID, "username", req.Username)
	c.JSON(http.StatusOK, gin.H{"id": u.ID})
}

func (p *Portal) handleLogin(c *gin.Context) {
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	req.Login = strings.TrimSpace(req.Login)
	if req.Login == "" || req.Password == "" {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "login/password 必填")
		return
	}
	// 账号维度失败锁定（08 §5.4：5 次失败锁 15 分钟；进程内实现，多副本改 Redis）
	if until, ok := p.blocked(req.Login); ok {
		writeAPIError(c, http.StatusTooManyRequests, "too_many_requests",
			"登录失败次数过多，请 "+until.UTC().Format("15:04")+" 后重试")
		return
	}
	ctx := c.Request.Context()
	u, err := p.Repo.UserByLogin(ctx, req.Login)
	if err != nil {
		p.log().Error("portal login lookup", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "登录暂不可用")
		return
	}
	fail := func() {
		p.recordFailure(req.Login)
		writePortalUnauthorized(c, "用户名、邮箱或密码不正确")
	}
	if u == nil || u.PasswordHash == nil {
		fail()
		return
	}
	if _, err := password.Verify(req.Password, *u.PasswordHash); err != nil {
		if errors.Is(err, password.ErrMismatch) {
			fail()
			return
		}
		p.log().Error("portal login verify", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "登录暂不可用")
		return
	}
	if u.Status != "active" {
		writePortalUnauthorized(c, "账号不可用")
		return
	}
	p.clearFailures(req.Login)
	_ = p.Repo.DeleteExpiredUserSessions(ctx, u.ID, time.Now().UTC())
	tok, err := sessionToken()
	if err != nil {
		p.log().Error("portal session token", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "登录失败")
		return
	}
	now := time.Now().UTC()
	sess := &model.AuthSession{TokenHash: auth.HashKey(p.Salt, tok), UserID: u.ID,
		IP: c.ClientIP(), UserAgent: truncateUA(c.Request.UserAgent()), ExpiresAt: now.Add(sessionTTL),
		LastActiveAt: now, CreatedAt: now}
	if err := p.Repo.CreateAuthSession(ctx, sess); err != nil {
		p.log().Error("portal login session", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "登录失败")
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: tok, Path: "/",
		HttpOnly: true, Secure: false, // Secure 由反向代理 TLS 终结时打开（本地 http 需 false）
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	c.JSON(http.StatusOK, gin.H{
		"token": tok, "expires_at": sess.ExpiresAt,
		"user": gin.H{"id": u.ID, "username": u.Username, "email": u.Email},
	})
}

func (p *Portal) handleLogout(c *gin.Context) {
	if h, ok := c.Get(sessionHashCtxKey); ok {
		_ = p.Repo.DeleteAuthSessionByHash(c.Request.Context(), h.(string))
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
	c.Status(http.StatusNoContent)
}

func truncateUA(s string) string {
	if len(s) <= 255 {
		return s
	}
	return s[:255]
}
