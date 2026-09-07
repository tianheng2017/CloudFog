package httpserver

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"cloudfog/internal/model"
	"cloudfog/internal/pkg/password"
	"cloudfog/internal/repository"
)

// userCreateReq 管理端创建用户（07 §4.1 POST /users）。
// 管理创建绕过邮箱验证（status 直置 active）；仅 super_admin 可创建 admin/super_admin 账号。
type userCreateReq struct {
	Username       string `json:"username"`
	Email          string `json:"email"`
	Password       string `json:"password"`
	Role           string `json:"role"`
	DefaultGroupID *int64 `json:"default_group_id"`
}

func (a *Admin) handleUserCreate(c *gin.Context) {
	var req userCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	// 与注册一致：username/email 统一小写存储（登录按 lower 比对，防大小写变体歧义）
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Username == "" || req.Email == "" || !strings.Contains(req.Email, "@") || req.Password == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "username/email/password 必填且 email 格式须含 @")
		return
	}
	if len(req.Password) < 8 {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "密码至少 8 位")
		return
	}
	if req.Role == "" {
		req.Role = roleUser
	}
	if req.Role != roleUser && req.Role != roleAdmin && req.Role != roleSuper {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "非法角色")
		return
	}
	// 造更高角色账号仅 super_admin（防 admin 横向提权）
	if (req.Role == roleAdmin || req.Role == roleSuper) && !isSuper(c) {
		a.deny(c, "user.create", "user", "", "创建 admin/super_admin 账号需 super_admin")
		return
	}
	if req.DefaultGroupID != nil {
		g, err := a.Repo.GroupByID(c.Request.Context(), *req.DefaultGroupID)
		if err != nil {
			a.log().Error("admin user create group", "error", err)
			writeAdminError(c, http.StatusInternalServerError, "server_error", "分组校验失败")
			return
		}
		if g == nil {
			writeAdminError(c, http.StatusNotFound, "not_found", "默认分组不存在")
			return
		}
	}
	hash, err := password.Hash(req.Password)
	if err != nil {
		a.log().Error("admin user create hash", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	u := &model.User{Username: req.Username, Email: req.Email, PasswordHash: &hash,
		PasswordAlgo: "argon2id", Role: req.Role, Status: "active", DefaultGroupID: req.DefaultGroupID,
		Timezone: "UTC"}
	if err := a.Repo.CreateUser(c.Request.Context(), u); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "用户名/邮箱已被占用")
			return
		}
		a.log().Error("admin user create", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	// 默认分组写入可见集合（与注册路径一致——否则管理建用户 /me/groups 空白、只能 default 分支建 Key）
	if req.DefaultGroupID != nil {
		_ = a.Repo.AllowUserGroup(c.Request.Context(), u.ID, *req.DefaultGroupID)
	}
	_ = a.audit(c, auditEntry{Action: "user.create", TargetType: "user", TargetID: idStr(u.ID),
		After: gin.H{"username": req.Username, "role": req.Role}}, "success")
	c.JSON(http.StatusOK, gin.H{"id": u.ID})
}

// keySummary 密钥脱敏投影（管理/自助共用）：key_hash 与明文绝不出（08 §4.1：前缀+****）。
func keySummary(k model.APIKey) gin.H {
	var expires, last any
	if k.ExpiresAt != nil {
		expires = *k.ExpiresAt
	}
	if k.LastUsedAt != nil {
		last = *k.LastUsedAt
	}
	return gin.H{
		"id": k.ID, "name": k.Name, "key_prefix": k.KeyPrefix, "status": k.Status,
		"expires_at": expires, "last_used_at": last, "created_at": k.CreatedAt,
		"ip_whitelist": k.IPWhitelist, "model_whitelist": k.ModelWhitelist,
	}
}

// handleUserKeys 用户密钥列表（脱敏：key_hash/明文绝不出，仅前缀与元数据）。
func (a *Admin) handleUserKeys(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	u, err := a.Repo.UserByID(c.Request.Context(), id)
	if err != nil {
		a.log().Error("admin user keys get user", "id", id, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	if u == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "用户不存在")
		return
	}
	ks, err := a.Repo.ListUserKeys(c.Request.Context(), id)
	if err != nil {
		a.log().Error("admin user keys list", "id", id, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	items := make([]gin.H, 0, len(ks))
	for _, k := range ks {
		items = append(items, keySummary(k))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
