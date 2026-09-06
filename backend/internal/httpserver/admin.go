package httpserver

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"cloudfog/internal/auth"
	"cloudfog/internal/model"
	"cloudfog/internal/repository"
)

// 角色常量（与 model.User.Role 值一致，08 §5.2）。
const (
	roleUser   = "user"
	roleAdmin  = "admin"
	roleSuper  = "super_admin"
	actorAdmin = "admin"
	actorSys   = "system"
)

// manualAdjustLimit 手动调账单笔限额（07 §4.1：|amount| 超限需 super_admin）。
var manualAdjustLimit = decimal.NewFromInt(500)

// Admin 管理端 API（07 §4：/api/v1/admin）。
// 管理操作鉴权走管理账号的 API Key（Bearer）+ 角色强校验；与 v1 用户组物理分离。
type Admin struct {
	Repo   *repository.Repository
	Store  auth.Lookup
	Salt   string
	Log    *slog.Logger
	MK     string // 凭证信封主密钥（security.master_key）
	MKPrev string // 轮换期上一代（可空）
}

func (a *Admin) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

// Register 挂载 /api/v1/admin 组：RequestID → APIKeyAuth → RequireAdmin。
func (a *Admin) Register(eng *gin.Engine) {
	g := eng.Group("/api/v1/admin")
	g.Use(RequestIDMiddleware(), APIKeyAuth(a.Store, a.Salt), RequireAdmin())
	// 用户与分组
	g.GET("/users", a.handleUsersList)
	g.GET("/users/:id", a.handleUserGet)
	g.PATCH("/users/:id", a.handleUserPatch)
	g.POST("/users/:id/disable", a.handleUserDisable)
	g.POST("/users/:id/enable", a.handleUserEnable)
	g.PATCH("/users/:id/role", a.handleUserRole)
	g.GET("/users/:id/groups", a.handleUserGroupsGet)
	g.PUT("/users/:id/groups", a.handleUserGroupsPut)
	g.POST("/users/:id/balance", a.handleUserBalance)
	g.GET("/groups", a.handleGroupsList)
	g.POST("/groups", a.handleGroupCreate)
	g.GET("/groups/:id", a.handleGroupGet)
	g.PATCH("/groups/:id", a.handleGroupPatch)
	g.DELETE("/groups/:id", a.handleGroupDelete)
}

// RequireAdmin 角色强制校验（08 §5.2/§5.3：前端权限仅为体验优化）。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := PrincipalOf(c)
		if !ok {
			abortAuth(c, http.StatusUnauthorized, auth.CodeInvalidAPIKey, "缺少管理身份")
			return
		}
		if p.User.Role != roleAdmin && p.User.Role != roleSuper {
			writeAdminError(c, http.StatusForbidden, "permission_denied", "需要 admin/super_admin 角色")
			return
		}
		c.Next()
	}
}

// isSuper 当前主体是否 super_admin。
func isSuper(c *gin.Context) bool {
	p, ok := PrincipalOf(c)
	return ok && p.User.Role == roleSuper
}

// targetRequiresSuper 等级保护（08 §5.3）：目标为 super_admin 时仅 super_admin 可管理，
// 普通 admin 不得禁用/改资料/调账/改分组等操作超管账户（越权即横向提权）。
func targetRequiresSuper(c *gin.Context, targetRole string) bool {
	return targetRole == roleSuper && !isSuper(c)
}

// deny 记录越权/限权尝试（审计 failure）并返回 403。
func (a *Admin) deny(c *gin.Context, action, targetType, targetID, reason string) {
	_ = a.audit(c, auditEntry{Action: action, TargetType: targetType, TargetID: targetID,
		After: map[string]any{"reason": reason, "denied": true}}, "failure")
	writeAdminError(c, http.StatusForbidden, "permission_denied", reason)
}

// actorOf 管理操作主体（审计 ActorID/ActorType）。
func actorOf(c *gin.Context) (*int64, string) {
	if p, ok := PrincipalOf(c); ok {
		id := p.User.ID
		return &id, actorAdmin
	}
	return nil, actorSys
}

// writeAdminError 管理端错误体（与对外同构：{error:{code,message,request_id}}）。
func writeAdminError(c *gin.Context, status int, code, msg string) {
	writeAPIError(c, status, code, msg)
}

// auditEntry 审计记录参数（action 与权限点同命名空间 08 §5.2）。
type auditEntry struct {
	Action     string
	TargetType string
	TargetID   string
	Before     map[string]any
	After      map[string]any
}

func (a *Admin) audit(c *gin.Context, e auditEntry, result string) error {
	id, typ := actorOf(c)
	rec := &model.AuditLog{
		ActorID:    id,
		ActorType:  typ,
		Action:     e.Action,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
		Before:     e.Before,
		After:      e.After,
		Result:     result,
		ClientIP:   maskIP(c.ClientIP()),
		UserAgent:  truncate(c.Request.UserAgent(), 512),
	}
	if err := a.Repo.InsertAudit(c.Request.Context(), rec); err != nil {
		// 审计不可静默丢失（08 §8）：操作可能已生效，此处必须留痕告警供人工核查。
		a.log().Error("admin 审计写入失败（操作可能已生效，需人工核查）",
			"action", e.Action, "target", e.TargetType+":"+e.TargetID, "result", result, "error", err)
		return err
	}
	return nil
}

// maskIP 客户端 IP 掩码（08 §8：IPv4 末段、IPv6 后 64 位置零）。
func maskIP(ip string) string {
	ip = strings.TrimSpace(ip)
	host := ip
	if h, _, err := net.SplitHostPort(ip); err == nil {
		host = h
	}
	parsed := net.ParseIP(host)
	if parsed == nil {
		return ""
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String()
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// pathID 取 :id 并校验正数。
func pathID(c *gin.Context) (int64, bool) {
	var id int64
	if err := parseID(c.Param("id"), &id); err != nil || id <= 0 {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "非法资源 id")
		return 0, false
	}
	return id, true
}

// ── 用户管理（07 §4.1）──────────────────────────────────────

type usersListQuery struct {
	Role    string `form:"role"`
	Status  string `form:"status"`
	Keyword string `form:"keyword"`
	GroupID int64  `form:"group_id"`
	Offset  int    `form:"offset"`
	Limit   int    `form:"limit"`
}

func (a *Admin) handleUsersList(c *gin.Context) {
	var q usersListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "查询参数非法")
		return
	}
	f := repository.UserListFilter{Role: q.Role, Status: q.Status, Keyword: q.Keyword, Offset: q.Offset, Limit: q.Limit}
	if q.GroupID > 0 {
		f.GroupID = &q.GroupID
	}
	users, total, err := a.Repo.ListUsers(c.Request.Context(), f)
	if err != nil {
		a.log().Error("admin users list", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "用户列表查询失败")
		return
	}
	items := make([]gin.H, 0, len(users))
	for _, u := range users {
		items = append(items, adminUserSummary(u))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "offset": q.Offset})
}

func adminUserSummary(u model.User) gin.H {
	return gin.H{
		"id": u.ID, "email": u.Email, "username": u.Username,
		"role": u.Role, "status": u.Status, "risk_level": u.RiskLevel,
		"created_at": u.CreatedAt,
	}
}

func (a *Admin) handleUserGet(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	u, err := a.Repo.UserByID(c.Request.Context(), id)
	if err != nil || u == nil {
		if err != nil {
			a.log().Error("admin user get", "id", id, "error", err)
			writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
			return
		}
		writeAdminError(c, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	// 含余额与 Key 数（脱敏列表）
	bal, err := a.Repo.BalanceByUserID(c.Request.Context(), id)
	if err != nil {
		a.log().Error("admin user balance", "id", id, "error", err)
	}
	balance := "0"
	if bal != nil {
		balance = bal.Balance.String()
	}
	c.JSON(http.StatusOK, gin.H{
		"id": u.ID, "email": u.Email, "username": u.Username,
		"role": u.Role, "status": u.Status, "risk_level": u.RiskLevel,
		"timezone": u.Timezone, "concurrency_limit": u.ConcurrencyLimit,
		"balance": balance, "version": u.Version, "created_at": u.CreatedAt,
	})
}

func (a *Admin) handleUserPatch(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var patch repository.UserProfilePatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	// 空 patch（无任何可改字段）显式拒绝，避免"成功但无变更"的空审计
	if fields, err := patch.Apply(); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	} else if len(fields) == 0 {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "没有可更新的字段")
		return
	}
	// 快照 before（审计非敏感字段）
	prev, _ := a.Repo.UserByID(c.Request.Context(), id)
	if prev == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	if targetRequiresSuper(c, prev.Role) {
		a.deny(c, "user.update", "user", idStr(id), "无权修改 super_admin 用户资料")
		return
	}
	if err := a.Repo.UpdateUserProfile(c.Request.Context(), id, patch); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "用户名/邮箱/电话已被其他用户占用")
			return
		}
		// 不向客户端回显底层 DB 错误（08 §10：对外不暴露内部路径/约束细节）
		a.log().Error("admin user patch", "id", id, "error", err)
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "更新失败")
		return
	}
	after := gin.H{"timezone": nil}
	if patch.Timezone != nil {
		after["timezone"] = *patch.Timezone
	}
	if patch.ConcurrencyLimit != nil {
		after["concurrency_limit"] = *patch.ConcurrencyLimit
	}
	_ = a.audit(c, auditEntry{Action: "user.update", TargetType: "user", TargetID: idStr(id),
		Before: gin.H{"status": prev.Status, "role": prev.Role}, After: after}, "success")
	c.Status(http.StatusOK)
}

func (a *Admin) handleUserDisable(c *gin.Context) {
	a.setUserActive(c, "disabled")
}

func (a *Admin) handleUserEnable(c *gin.Context) {
	a.setUserActive(c, "active")
}

// setUserActive disable 必须带 reason（审计；enable 不要求）。
func (a *Admin) setUserActive(c *gin.Context, status string) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if status == "disabled" {
		if err := c.ShouldBindJSON(&body); err != nil {
			writeAdminError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
			return
		}
		if r := strings.TrimSpace(body.Reason); r == "" {
			writeAdminError(c, http.StatusBadRequest, "invalid_request", "禁用必须填写原因")
			return
		} else if len(r) > 500 {
			writeAdminError(c, http.StatusBadRequest, "invalid_request", "原因不能超过 500 字符")
			return
		}
	}
	prev, _ := a.Repo.UserByID(c.Request.Context(), id)
	if prev == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	action := "user.enable"
	if status == "disabled" {
		action = "user.disable"
	}
	if status == "disabled" {
		if actorID, _ := actorOf(c); actorID != nil && *actorID == id {
			// 防管理真空：admin/super 禁用自身会使自己凭据即刻失效且（若为最后超管）平台无人可管。
			_ = a.audit(c, auditEntry{Action: action, TargetType: "user", TargetID: idStr(id),
				After: map[string]any{"reason": "self_disable_denied", "denied": true}}, "failure")
			writeAdminError(c, http.StatusBadRequest, "invalid_request", "不允许禁用自身账户（需另一管理员操作）")
			return
		}
	}
	if targetRequiresSuper(c, prev.Role) {
		a.deny(c, action, "user", idStr(id), "无权启停 super_admin 用户")
		return
	}
	if err := a.Repo.SetUserStatus(c.Request.Context(), id, status); err != nil {
		a.log().Error("admin user status", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "状态变更失败")
		return
	}
	after := map[string]any{"status": status}
	if status == "disabled" {
		after["reason"] = body.Reason
	}
	_ = a.audit(c, auditEntry{Action: action, TargetType: "user", TargetID: idStr(id),
		Before: map[string]any{"status": prev.Status}, After: after}, "success")
	c.Status(http.StatusOK)
}

func (a *Admin) handleUserRole(c *gin.Context) {
	if !isSuper(c) {
		writeAdminError(c, http.StatusForbidden, "permission_denied", "修改角色仅限 super_admin")
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	actorID, _ := actorOf(c)
	if actorID != nil && *actorID == id {
		// 防误锁死：super 不得修改自身角色（含自降级）；换人操作或直接改库。
		_ = a.audit(c, auditEntry{Action: "user.role.update", TargetType: "user", TargetID: idStr(id),
			After: map[string]any{"reason": "self_role_change_denied", "denied": true}}, "failure")
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "不允许修改自身角色（需另一超管操作）")
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Role == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "role 必填")
		return
	}
	if body.Role != roleUser && body.Role != roleAdmin && body.Role != roleSuper {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "非法角色")
		return
	}
	if err := a.Repo.SetUserRole(c.Request.Context(), id, body.Role); err != nil {
		writeAdminError(c, http.StatusInternalServerError, "server_error", "角色变更失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "user.role.update", TargetType: "user", TargetID: idStr(id),
		Before: gin.H{}, After: gin.H{"role": body.Role}}, "success")
	c.Status(http.StatusOK)
}

// handleUserBalance 手动调账（必填原因；|amount|>限额需 super_admin；写 billing_ledger + audit）。
func (a *Admin) handleUserBalance(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var body struct {
		Amount string `json:"amount"` // decimal 字符串，符号表方向
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Reason) == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "amount/reason 必填")
		return
	}
	amount, err := decimal.NewFromString(body.Amount)
	if err != nil || amount.IsZero() {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "amount 非法或为 0")
		return
	}
	if r := strings.TrimSpace(body.Reason); r == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "调账必须填写原因")
		return
	} else if len(r) > 500 {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "原因不能超过 500 字符")
		return
	}
	if amount.Abs().GreaterThan(manualAdjustLimit) && !isSuper(c) {
		a.deny(c, "user.balance.adjust", "user", idStr(id), "单笔超过限额需 super_admin")
		return
	}
	prev, _ := a.Repo.UserByID(c.Request.Context(), id)
	if prev == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	// 等级保护 + 防自充值：admin 不得调 super_admin 账；任何人不得调自身账（防止以管理密钥自充，审计留痕改为他操作或走正式充值）。
	opID, _ := actorOf(c)
	if targetRequiresSuper(c, prev.Role) {
		a.deny(c, "user.balance.adjust", "user", idStr(id), "无权调整 super_admin 账户余额")
		return
	}
	if opID != nil && *opID == id {
		_ = a.audit(c, auditEntry{Action: "user.balance.adjust", TargetType: "user", TargetID: idStr(id),
			After: map[string]any{"reason": "self_adjust_denied", "denied": true}}, "failure")
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "不允许调整自身账户余额")
		return
	}
	if err := a.Repo.ManualBalanceAdjust(c.Request.Context(), id, amount, body.Reason, opID); err != nil {
		if errors.Is(err, repository.ErrNegativeBalance) {
			writeAdminError(c, http.StatusBadRequest, "insufficient_balance", "调账后余额不可为负")
			return
		}
		a.log().Error("admin balance adjust", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "调账失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "user.balance.adjust", TargetType: "user", TargetID: idStr(id),
		Before: gin.H{}, After: gin.H{"amount": amount.String(), "reason": body.Reason}}, "success")
	c.Status(http.StatusOK)
}

// user allowed groups 读写
func (a *Admin) handleUserGroupsGet(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if u, _ := a.Repo.UserByID(c.Request.Context(), id); u == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	rows, err := a.Repo.UserAllowedGroupIDs(c.Request.Context(), id)
	if err != nil {
		a.log().Error("admin user groups", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"group_ids": rows})
}

func (a *Admin) handleUserGroupsPut(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var body struct {
		GroupIDs []int64 `json:"group_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "group_ids 必填")
		return
	}
	u, _ := a.Repo.UserByID(c.Request.Context(), id)
	if u == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	if targetRequiresSuper(c, u.Role) {
		a.deny(c, "user.groups.update", "user", idStr(id), "无权设置 super_admin 用户的分组")
		return
	}
	if err := a.Repo.ReplaceAllowedGroups(c.Request.Context(), id, body.GroupIDs); err != nil {
		a.log().Error("admin user groups put", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "分组设置失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "user.groups.update", TargetType: "user", TargetID: idStr(id),
		Before: gin.H{}, After: gin.H{"group_ids": body.GroupIDs}}, "success")
	c.Status(http.StatusOK)
}

func idStr(id int64) string {
	return strconv.FormatInt(id, 10)
}

func parseID(s string, out *int64) error {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*out = v
	return nil
}
