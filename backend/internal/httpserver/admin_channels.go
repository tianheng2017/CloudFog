package httpserver

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"cloudfog/internal/model"
	"cloudfog/internal/pkg/crypto"
	"cloudfog/internal/repository"
)

// channelReq 渠道创建/更新可编辑字段（金额/倍率一律十进制文本）。credentials 仅写入时出现（密封后永不明文返回）。
type channelReq struct {
	Name           *string        `json:"name"`
	ProviderCode   *string        `json:"provider_code"`
	BaseURL        *string        `json:"base_url"`
	AuthType       *string        `json:"auth_type"`
	Priority       *int           `json:"priority"`
	Weight         *int           `json:"weight"`
	Concurrency    *int           `json:"concurrency"`
	RateMultiplier *string        `json:"rate_multiplier"`
	TokenRatio     *string        `json:"token_ratio"`
	Schedulable    *bool          `json:"schedulable"`
	Credentials    map[string]any `json:"credentials,omitempty"` // 明文仅请求体存在；响应永不回显
}

func (r channelReq) toPatch() (repository.ChannelPatch, error) {
	p := repository.ChannelPatch{
		Name: r.Name, BaseURL: r.BaseURL, AuthType: r.AuthType,
		Priority: r.Priority, Weight: r.Weight, Concurrency: r.Concurrency,
		Schedulable: r.Schedulable, ProviderCode: r.ProviderCode,
	}
	if r.RateMultiplier != nil {
		d, err := decimal.NewFromString(*r.RateMultiplier)
		if err != nil {
			return p, err
		}
		s := d.String()
		p.RateMultiplier = &s
	}
	if r.TokenRatio != nil {
		d, err := decimal.NewFromString(*r.TokenRatio)
		if err != nil {
			return p, err
		}
		s := d.String()
		p.TokenRatio = &s
	}
	return p, nil
}

// channelJSON 脱敏输出：credentials 明文/密文一律不出（仅给 encrypted 标志 + key_id），08 §4.2/§10。
func channelJSON(c *model.Channel) gin.H {
	rate := "1"
	if !c.RateMultiplier.IsZero() {
		rate = c.RateMultiplier.String()
	}
	ratio := "1"
	if !c.TokenRatio.IsZero() {
		ratio = c.TokenRatio.String()
	}
	mask := gin.H{"encrypted": crypto.IsSealed(c.Credentials)}
	if c.CredKeyID != nil && *c.CredKeyID != "" {
		mask["key_id"] = *c.CredKeyID
	}
	return gin.H{
		"id": c.ID, "name": c.Name, "provider_code": c.ProviderCode,
		"base_url": c.BaseURL, "auth_type": c.AuthType,
		"priority": c.Priority, "weight": c.Weight, "concurrency": c.Concurrency,
		"rate_multiplier": rate, "token_ratio": ratio,
		"status": c.Status, "schedulable": c.Schedulable,
		"circuit_state": c.CircuitState, "health_score": c.HealthScore,
		"error_message": c.ErrorMessage, "expires_at": c.ExpiresAt,
		"cred": mask, "created_at": c.CreatedAt,
	}
}

// providerByCode 渠道所属供应商（不存在/已软删 → 404）。
func (a *Admin) providerByCode(c *gin.Context, code string) (*model.Provider, bool) {
	p, err := a.Repo.ProviderByCode(c.Request.Context(), code)
	if err != nil {
		a.log().Error("admin provider by code", "code", code, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return nil, false
	}
	if p == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "供应商不存在")
		return nil, false
	}
	return p, true
}

func (a *Admin) handleChannelsList(c *gin.Context) {
	var q struct {
		ProviderCode string `form:"provider_code"`
		Status       string `form:"status"`
		Name         string `form:"name"`
		GroupID      int64  `form:"group_id"`
		Offset       int    `form:"offset"`
		Limit        int    `form:"limit"`
	}
	_ = c.ShouldBindQuery(&q)
	f := repository.ChannelListFilter{ProviderCode: q.ProviderCode, Status: q.Status, Name: q.Name, Offset: q.Offset, Limit: q.Limit}
	if q.GroupID > 0 {
		f.GroupID = &q.GroupID
	}
	cs, total, err := a.Repo.ListChannels(c.Request.Context(), f)
	if err != nil {
		a.log().Error("admin channels list", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "渠道列表查询失败")
		return
	}
	items := make([]gin.H, 0, len(cs))
	for _, ch := range cs {
		items = append(items, channelJSON(&ch))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "offset": q.Offset})
}

func (a *Admin) handleChannelGet(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	ch, err := a.Repo.ChannelByID(c.Request.Context(), id)
	if err != nil || ch == nil {
		if err != nil {
			a.log().Error("admin channel get", "id", id, "error", err)
			writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
			return
		}
		writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
		return
	}
	groups, err := a.Repo.ChannelGroupsByChannel(c.Request.Context(), id)
	if err != nil {
		a.log().Error("admin channel get", "id", id, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	out := channelJSON(ch)
	out["group_ids"] = groups
	c.JSON(http.StatusOK, out)
}

// handleChannelCreate 建渠道：凭证经 seal 回调同事务落密文（明文零落库窗口）。
func (a *Admin) handleChannelCreate(c *gin.Context) {
	var req channelReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == nil || *req.Name == "" || req.ProviderCode == nil || *req.ProviderCode == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "name/provider_code 必填")
		return
	}
	prov, ok := a.providerByCode(c, *req.ProviderCode)
	if !ok {
		return
	}
	// 默认态必须可立即参与调度：active+schedulable+rate1+token1+priority50。
	// （DB 列默认值在 GORM struct 插入零值时不会生效，漏设会产生 status='' 永不调度 / rate=0 白送流量）
	ch := &model.Channel{Name: *req.Name, ProviderID: prov.ID, ProviderCode: prov.Code,
		BaseURL: req.BaseURL, AuthType: req.AuthType, // 覆盖 provider 默认端点/鉴权
		Status: "active", Schedulable: true, Priority: 50, Weight: 100, Concurrency: 3,
		RateMultiplier: model.Decimal{Decimal: decimal.NewFromInt(1)},
		TokenRatio:     model.Decimal{Decimal: decimal.NewFromInt(1)},
		Credentials:    req.Credentials} // seal 回调加密该明文；请求未带凭证则落空信封
	if ch.Credentials == nil {
		ch.Credentials = map[string]any{}
	}
	if req.Schedulable != nil {
		ch.Schedulable = *req.Schedulable
	}
	if req.Priority != nil {
		ch.Priority = *req.Priority
	}
	if req.RateMultiplier != nil {
		d, err := decimal.NewFromString(*req.RateMultiplier)
		if err != nil {
			writeAdminError(c, http.StatusBadRequest, "invalid_request", "rate_multiplier 须为合法十进制")
			return
		}
		ch.RateMultiplier = model.Decimal{Decimal: d}
	}
	if req.TokenRatio != nil {
		d, err := decimal.NewFromString(*req.TokenRatio)
		if err != nil {
			writeAdminError(c, http.StatusBadRequest, "invalid_request", "token_ratio 须为合法十进制")
			return
		}
		ch.TokenRatio = model.Decimal{Decimal: d}
	}
	id, err := a.Repo.CreateChannel(c.Request.Context(), ch, func(cid int64, plain map[string]any) (map[string]any, error) {
		return a.sealChannelCreds(cid, plain)
	})
	if err != nil {
		a.log().Error("admin channel create", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "channel.create", TargetType: "channel", TargetID: idStr(id), After: gin.H{"name": *req.Name, "provider_code": prov.Code}}, "success")
	out := gin.H{"id": id, "name": *req.Name, "provider_code": prov.Code, "cred": gin.H{"encrypted": true}}
	c.JSON(http.StatusOK, out)
}

// handleChannelPatch 更新基础字段 + 可选凭证旋转（credentials 出现即密封替换）。
func (a *Admin) handleChannelPatch(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req channelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	if cur, _ := a.Repo.ChannelByID(c.Request.Context(), id); cur == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
		return
	}
	p, err := req.toPatch()
	if err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "倍率/系数须为合法十进制")
		return
	}
	p.ProviderCode = nil // provider_code 变更需同步 provider_id，不允许经 PATCH 单独改（防不一致）
	if emptyPatch(p) && req.Credentials == nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "没有可更新的字段")
		return
	}
	if req.Credentials != nil {
		if err := a.Repo.SetChannelCredentials(c.Request.Context(), id, func(cid int64, plain map[string]any) (map[string]any, error) {
			return a.sealChannelCreds(cid, plain)
		}, req.Credentials); err != nil {
			a.log().Error("admin channel cred rotate", "id", id, "error", err)
			writeAdminError(c, http.StatusInternalServerError, "server_error", "凭证更新失败")
			return
		}
		_ = a.audit(c, auditEntry{Action: "channel.credential.rotate", TargetType: "channel", TargetID: idStr(id)}, "success")
	}
	if emptyPatch(p) {
		// 仅凭证轮换（无基础字段变更）→ 直接返回脱敏详情
		ch, _ := a.Repo.ChannelByID(c.Request.Context(), id)
		c.JSON(http.StatusOK, channelJSON(ch))
		return
	}
	if err := a.Repo.UpdateChannel(c.Request.Context(), id, p); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
			return
		}
		a.log().Error("admin channel patch", "id", id, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "更新失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "channel.update", TargetType: "channel", TargetID: idStr(id)}, "success")
	ch, _ := a.Repo.ChannelByID(c.Request.Context(), id)
	if ch != nil {
		c.JSON(http.StatusOK, channelJSON(ch))
		return
	}
	c.Status(http.StatusOK)
}

func emptyPatch(p repository.ChannelPatch) bool {
	return p.Name == nil && p.BaseURL == nil && p.AuthType == nil && p.Priority == nil &&
		p.Weight == nil && p.Concurrency == nil && p.Schedulable == nil && p.RateMultiplier == nil && p.TokenRatio == nil
}

func (a *Admin) handleChannelDelete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := a.Repo.DeleteChannel(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
			return
		}
		a.log().Error("admin channel delete", "id", id, "error", err)
		writeAdminError(c, http.StatusConflict, "conflict", "删除失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "channel.delete", TargetType: "channel", TargetID: idStr(id)}, "success")
	c.Status(http.StatusNoContent)
}

// channelToggle 启停（enable→active+schedulable；disable→disabled 停排）。
func (a *Admin) channelToggle(c *gin.Context, enabled bool) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := a.Repo.SetChannelEnabled(c.Request.Context(), id, enabled); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
			return
		}
		a.log().Error("admin channel toggle", "id", id, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "状态变更失败")
		return
	}
	action := "channel.disable"
	if enabled {
		action = "channel.enable"
	}
	_ = a.audit(c, auditEntry{Action: action, TargetType: "channel", TargetID: idStr(id)}, "success")
	c.Status(http.StatusOK)
}

func (a *Admin) handleChannelDisable(c *gin.Context) { a.channelToggle(c, false) }
func (a *Admin) handleChannelEnable(c *gin.Context)  { a.channelToggle(c, true) }

func (a *Admin) handleChannelCircuitReset(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := a.Repo.ResetChannelCircuit(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
			return
		}
		writeAdminError(c, http.StatusInternalServerError, "server_error", "熔断复位失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "channel.circuit.reset", TargetType: "channel", TargetID: idStr(id)}, "success")
	c.Status(http.StatusOK)
}

func (a *Admin) handleChannelGroupsGet(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if ch, err := a.Repo.ChannelByID(c.Request.Context(), id); err != nil || ch == nil {
		if err != nil {
			a.log().Error("admin channel groups get", "id", id, "error", err)
			writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
			return
		}
		writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
		return
	}
	gids, err := a.Repo.ChannelGroupsByChannel(c.Request.Context(), id)
	if err != nil {
		writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"group_ids": gids})
}

func (a *Admin) handleChannelGroupsPut(c *gin.Context) {
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
	if ch, err := a.Repo.ChannelByID(c.Request.Context(), id); err != nil || ch == nil {
		if err != nil {
			a.log().Error("admin channel groups put", "id", id, "error", err)
			writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
			return
		}
		writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
		return
	}
	if err := a.Repo.ReplaceChannelGroups(c.Request.Context(), id, body.GroupIDs); err != nil {
		if repository.IsForeignKeyViolation(err) {
			writeAdminError(c, http.StatusNotFound, "not_found", "存在不存在的分组")
			return
		}
		a.log().Error("admin channel groups put", "id", id, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "分组设置失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "channel.groups.update", TargetType: "channel", TargetID: idStr(id), After: gin.H{"group_ids": body.GroupIDs}}, "success")
	c.Status(http.StatusOK)
}

// handleChannelTest 连通测试（07 §4.2）：解密凭证后请求 {base}/models，返回状态与延迟。
// MVP 覆盖 openai_compat（GET /models）；其它协议返回 unsupported（探测层按 05 演进补全）。
func (a *Admin) handleChannelTest(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	ch, err := a.Repo.ChannelByID(c.Request.Context(), id)
	if err != nil || ch == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "渠道不存在")
		return
	}
	prov, _ := a.Repo.ProviderByCode(c.Request.Context(), ch.ProviderCode)
	if prov == nil {
		writeAdminError(c, http.StatusConflict, "conflict", "所属供应商不存在")
		return
	}
	if prov.Protocol != "openai_compat" {
		writeAdminError(c, http.StatusNotImplemented, "unsupported", "该协议暂不支持连通测试（探测层演进后补全）")
		return
	}
	cred := ch.Credentials
	if crypto.IsSealed(cred) {
		out, err := crypto.Open(cred, a.MK, a.MKPrev, fmt.Sprintf("channel:%d", id))
		if err != nil {
			_ = a.audit(c, auditEntry{Action: "channel.test", TargetType: "channel", TargetID: idStr(id), After: gin.H{"error": "credential_decrypt_failed"}}, "failure")
			writeAdminError(c, http.StatusBadRequest, "invalid_request", "凭证解密失败（主密钥/轮换配置异常）")
			return
		}
		cred = out
	}
	base := prov.BaseURL
	if ch.BaseURL != nil && *ch.BaseURL != "" {
		base = *ch.BaseURL
	}
	url := strings.TrimRight(base, "/") + "/models"
	start := time.Now()
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, url, nil)
	if v, ok2 := cred["api_key"].(string); ok2 && v != "" {
		req.Header.Set("Authorization", "Bearer "+v)
	}
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		_ = a.audit(c, auditEntry{Action: "channel.test", TargetType: "channel", TargetID: idStr(id), After: gin.H{"error": err.Error()}}, "failure")
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "连接失败", "latency_ms": int(time.Since(start).Milliseconds())})
		return
	}
	defer func() { _ = resp.Body.Close() }()
	okFlag := resp.StatusCode < 400
	if !okFlag {
		_ = a.audit(c, auditEntry{Action: "channel.test", TargetType: "channel", TargetID: idStr(id), After: gin.H{"status": resp.StatusCode}}, "failure")
	} else {
		_ = a.audit(c, auditEntry{Action: "channel.test", TargetType: "channel", TargetID: idStr(id), After: gin.H{"status": resp.StatusCode}}, "success")
	}
	c.JSON(http.StatusOK, gin.H{"ok": okFlag, "status": resp.StatusCode, "latency_ms": int(time.Since(start).Milliseconds())})
}
