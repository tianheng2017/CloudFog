package httpserver

import (
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"cloudfog/internal/auth"
	"cloudfog/internal/model"
	"cloudfog/internal/pkg/apikey"
	"cloudfog/internal/repository"
)

const (
	maxKeysPerUser = 50 // 08 §3.3 单用户 Key 数量上限
	maxKeyNameLen  = 100
	maxKeyExpiry   = 2 * 365 * 24 * time.Hour // 08 §3.3 Key 有效期最长 2 年
)

// currentUser Portal 主体（SessionAuth 已注入）；非法时中止并返回 false。
func currentUser(c *gin.Context) (*model.User, bool) {
	pr, ok := PrincipalOf(c)
	if !ok || pr.User == nil {
		abortAuth(c, http.StatusUnauthorized, auth.CodeSessionExpired, "缺少会话身份")
		return nil, false
	}
	return pr.User, true
}

func (p *Portal) handleMyKeysList(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	ks, err := p.Repo.ListUserKeys(c.Request.Context(), u.ID)
	if err != nil {
		p.log().Error("portal me keys list", "user_id", u.ID, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	items := make([]gin.H, 0, len(ks))
	for _, k := range ks {
		it := keySummary(k)
		it["group_id"] = k.GroupID
		items = append(items, it)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// handleMyKeysCreate 创建 Key：唯一一次返回明文（08 §3.1）。分组须为可见（allowed ∪ 默认）。
func (p *Portal) handleMyKeysCreate(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	var req struct {
		Name           string   `json:"name"`
		GroupID        *int64   `json:"group_id"`
		ExpiresAt      *string  `json:"expires_at"` // RFC3339，最长 2 年
		IPWhitelist    []string `json:"ip_whitelist"`
		ModelWhitelist []string `json:"model_whitelist"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "default"
	}
	if len(req.Name) > maxKeyNameLen {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "Key 名称过长（≤100）")
		return
	}
	// 有效期：可选，now < t ≤ now+2y
	var expires *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeAPIError(c, http.StatusBadRequest, "invalid_request", "expires_at 需 RFC3339")
			return
		}
		now := time.Now().UTC()
		if !t.After(now) || t.After(now.Add(maxKeyExpiry)) {
			writeAPIError(c, http.StatusBadRequest, "invalid_request", "有效期须在未来且不超过 2 年")
			return
		}
		expires = &t
	}
	for _, cidr := range req.IPWhitelist {
		if _, err := parseIPOrPrefix(cidr); err != nil {
			writeAPIError(c, http.StatusBadRequest, "invalid_request", "ip_whitelist 含非法 IP/CIDR: "+cidr)
			return
		}
	}
	ctx := c.Request.Context()
	// 分组：默认分组优先；显式 group_id 必须在可见集合内
	gid := u.DefaultGroupID
	if req.GroupID != nil {
		gid = req.GroupID
	}
	if gid == nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "缺少分组（从 /me/groups 选择 group_id）")
		return
	}
	allowed, err := p.Repo.UserAllowedGroups(ctx, u.ID)
	if err != nil {
		p.log().Error("portal me keys allowed", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "分组查询失败")
		return
	}
	inAllowed := false
	for _, g := range allowed {
		if g.ID == *gid {
			inAllowed = true
			break
		}
	}
	usable := inAllowed || (u.DefaultGroupID != nil && *u.DefaultGroupID == *gid)
	if !usable {
		writeAPIError(c, http.StatusForbidden, "permission_denied", "无权使用该分组")
		return
	}
	// 分组须仍可用：default 分支未走 allowed（allowed 仅含 active），disabled 组会产出"活 Key 死分组"
	// → 调用时 Authenticate 因 group 不可用失败，拒绝在此创建而非制造静默失效 Key。
	grp, err := p.Repo.GroupByID(ctx, *gid)
	if err != nil {
		p.log().Error("portal me keys group", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "分组校验失败")
		return
	}
	if grp == nil || grp.Status != "active" {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "分组不可用（已停用）")
		return
	}
	cnt, err := p.Repo.CountUserKeys(ctx, u.ID)
	if err != nil {
		p.log().Error("portal me keys count", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	if cnt >= maxKeysPerUser {
		writeAPIError(c, http.StatusConflict, "key_limit_exceeded", "密钥数量已达上限（50）")
		return
	}
	plain, prefix, err := apikey.Generate()
	if err != nil {
		p.log().Error("portal me keys gen", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	k := &model.APIKey{UserID: u.ID, GroupID: *gid, Name: req.Name,
		KeyPrefix: prefix, KeyHash: auth.HashKey(p.Salt, plain), Status: "active",
		ExpiresAt: expires, IPWhitelist: req.IPWhitelist, ModelWhitelist: req.ModelWhitelist}
	if err := p.Repo.CreateAPIKey(ctx, k); err != nil {
		p.log().Error("portal me keys create", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	p.log().Info("portal me key created", "user_id", u.ID, "key_id", k.ID, "group_id", *gid)
	// token 唯一一次明文返回（08 §3.1）；此后仅剩哈希，无法找回
	c.JSON(http.StatusOK, gin.H{
		"id": k.ID, "name": k.Name, "group_id": *gid, "token": plain,
		"key_prefix": prefix, "status": "active", "expires_at": expires,
	})
}

func (p *Portal) handleMyKeysPatch(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req repository.APIKeySelfPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "disabled" {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "status 仅支持 active/disabled")
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > maxKeyNameLen {
			writeAPIError(c, http.StatusBadRequest, "invalid_request", "name 非法")
			return
		}
		req.Name = &name
	}
	m, err := req.Apply()
	if err != nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "参数非法")
		return
	}
	updated, err := p.Repo.UpdateAPIKeySelf(c.Request.Context(), id, u.ID, m)
	if err != nil {
		p.log().Error("portal me keys patch", "id", id, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "更新失败")
		return
	}
	if !updated {
		writeAPIError(c, http.StatusNotFound, "not_found", "密钥不存在")
		return
	}
	c.Status(http.StatusOK)
}

func (p *Portal) handleMyKeysDelete(c *gin.Context) {
	u, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	deleted, err := p.Repo.DeleteAPIKeyByUser(c.Request.Context(), id, u.ID)
	if err != nil {
		p.log().Error("portal me keys delete", "id", id, "error", err)
		writeAPIError(c, http.StatusInternalServerError, "server_error", "删除失败")
		return
	}
	if !deleted {
		writeAPIError(c, http.StatusNotFound, "not_found", "密钥不存在")
		return
	}
	c.Status(http.StatusNoContent)
}

// parseIPOrPrefix 校验裸 IP 或 CIDR。
func parseIPOrPrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if a, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	return netip.ParsePrefix(s)
}
