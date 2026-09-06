package httpserver

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"cloudfog/internal/model"
	"cloudfog/internal/repository"
)

// groupReq 创建/更新的可改字段（金额/倍率一律十进制文本，规避 JSON float 精度）。
type groupReq struct {
	Name             *string   `json:"name"`
	Status           *string   `json:"status"`
	RateMultiplier   *string   `json:"rate_multiplier"`
	AllowedModels    *[]string `json:"allowed_models"`
	FallbackModels   *[]string `json:"fallback_models"`
	RPMLimit         *int      `json:"rpm_limit"`
	ConcurrencyLimit *int      `json:"concurrency_limit"`
	DailyQuotaUSD    *string   `json:"daily_quota_usd"`
	SortOrder        *int      `json:"sort_order"`
}

func (r groupReq) toPatch() (repository.GroupPatch, error) {
	p := repository.GroupPatch{
		Name: r.Name, Status: r.Status, AllowedModels: r.AllowedModels,
		FallbackModels: r.FallbackModels, RPMLimit: r.RPMLimit,
		ConcurrencyLimit: r.ConcurrencyLimit, SortOrder: r.SortOrder,
	}
	if r.RateMultiplier != nil {
		d, err := decimal.NewFromString(*r.RateMultiplier)
		if err != nil {
			return p, err
		}
		if d.IsNegative() {
			return p, errors.New("rate_multiplier 不能为负（负倍率会反转计费方向）")
		}
		p.RateMultiplier = &d
	}
	if r.DailyQuotaUSD != nil {
		d, err := decimal.NewFromString(*r.DailyQuotaUSD)
		if err != nil {
			return p, err
		}
		if d.IsNegative() {
			return p, errors.New("daily_quota_usd 不能为负")
		}
		p.DailyQuotaUSD = &d
	}
	return p, nil
}

func groupJSON(g model.Group) gin.H {
	rate := "1"
	if !g.RateMultiplier.IsZero() {
		rate = g.RateMultiplier.String()
	}
	quota := ""
	if g.DailyQuotaUSD != nil {
		quota = g.DailyQuotaUSD.String()
	}
	return gin.H{
		"id": g.ID, "name": g.Name, "status": g.Status,
		"rate_multiplier": rate, "allowed_models": g.AllowedModels,
		"fallback_models": g.FallbackModels, "rpm_limit": g.RPMLimit,
		"concurrency_limit": g.ConcurrencyLimit, "daily_quota_usd": quota,
		"sort_order": g.SortOrder, "created_at": g.CreatedAt,
	}
}

func (a *Admin) handleGroupsList(c *gin.Context) {
	var q struct {
		Status string `form:"status"`
		Offset int    `form:"offset"`
		Limit  int    `form:"limit"`
	}
	_ = c.ShouldBindQuery(&q)
	gs, total, err := a.Repo.ListGroups(c.Request.Context(), repository.GroupListFilter{Status: q.Status, Offset: q.Offset, Limit: q.Limit})
	if err != nil {
		a.log().Error("admin groups list", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "分组列表查询失败")
		return
	}
	items := make([]gin.H, 0, len(gs))
	for _, g := range gs {
		items = append(items, groupJSON(g))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "offset": q.Offset})
}

func (a *Admin) handleGroupGet(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	g, err := a.Repo.GroupByID(c.Request.Context(), id)
	if err != nil || g == nil {
		if err != nil {
			a.log().Error("admin group get", "error", err)
			writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
			return
		}
		writeAdminError(c, http.StatusNotFound, "not_found", "分组不存在")
		return
	}
	chans, _ := a.Repo.ChannelGroupIDs(c.Request.Context(), id)
	out := groupJSON(*g)
	out["channel_ids"] = chans
	c.JSON(http.StatusOK, out)
}

func (a *Admin) handleGroupCreate(c *gin.Context) {
	var req groupReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == nil || *req.Name == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "name 必填")
		return
	}
	p, err := req.toPatch()
	if err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "倍率/额度须为合法十进制")
		return
	}
	g := &model.Group{Name: *req.Name}
	if req.Status != nil {
		g.Status = *req.Status
	} else {
		g.Status = "active"
	}
	if p.RateMultiplier != nil {
		g.RateMultiplier = model.Decimal{Decimal: *p.RateMultiplier}
	}
	if err := a.Repo.CreateGroup(c.Request.Context(), g); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "分组名已存在")
			return
		}
		a.log().Error("admin group create", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	// 创建后一次性应用全部可选字段（fallback/rpm/并发/quota/sort 等）——此前仅补 allowed_models 会静默丢字段。
	if err := a.Repo.UpdateGroup(c.Request.Context(), g.ID, p); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "分组名已存在")
			return
		}
		a.log().Error("admin group create apply", "id", g.ID, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "group.create", TargetType: "group", TargetID: idStr(g.ID), After: gin.H{"name": g.Name}}, "success")
	c.JSON(http.StatusOK, groupJSON(*g))
}

func (a *Admin) handleGroupPatch(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req groupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	p, err := req.toPatch()
	if err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "倍率/额度须为合法十进制")
		return
	}
	prev, _ := a.Repo.GroupByID(c.Request.Context(), id)
	if prev == nil {
		writeAdminError(c, http.StatusNotFound, "not_found", "分组不存在")
		return
	}
	if err := a.Repo.UpdateGroup(c.Request.Context(), id, p); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "分组名已存在")
			return
		}
		a.log().Error("admin group patch", "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "更新失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "group.update", TargetType: "group", TargetID: idStr(id), Before: gin.H{"name": prev.Name}, After: gin.H{}}, "success")
	g, _ := a.Repo.GroupByID(c.Request.Context(), id)
	if g != nil {
		c.JSON(http.StatusOK, groupJSON(*g))
		return
	}
	c.Status(http.StatusOK)
}

func (a *Admin) handleGroupDelete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := a.Repo.DeleteGroup(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "分组不存在")
			return
		}
		a.log().Error("admin group delete", "error", err)
		writeAdminError(c, http.StatusConflict, "conflict", "删除失败（可能仍被渠道/用户引用）")
		return
	}
	_ = a.audit(c, auditEntry{Action: "group.delete", TargetType: "group", TargetID: idStr(id)}, "success")
	c.Status(http.StatusNoContent)
}
