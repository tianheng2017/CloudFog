package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"cloudfog/internal/model"
	"cloudfog/internal/repository"
)

// ── 供应商（07 §4.2）────────────────────────────────────────

type providerReq struct {
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Protocol      string   `json:"protocol"`
	BaseURL       string   `json:"base_url"`
	AuthType      string   `json:"auth_type"`
	Capabilities  []string `json:"capabilities"`
	Status        string   `json:"status"`
	BillOnFailure *bool    `json:"bill_on_failure"`
}

func (a *Admin) handleProvidersList(c *gin.Context) {
	inc := c.Query("include_disabled") == "1" || c.Query("include_disabled") == "true"
	ps, err := a.Repo.ListProviders(c.Request.Context(), inc)
	if err != nil {
		writeAdminError(c, http.StatusInternalServerError, "server_error", "供应商列表失败")
		return
	}
	items := make([]gin.H, 0, len(ps))
	for _, p := range ps {
		items = append(items, providerJSON(p))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func providerJSON(p model.Provider) gin.H {
	return gin.H{"code": p.Code, "name": p.Name, "protocol": p.Protocol, "base_url": p.BaseURL,
		"auth_type": p.AuthType, "capabilities": p.Capabilities, "status": p.Status,
		"bill_on_failure": p.BillOnFailure, "usage_reliable": p.UsageReliable}
}

func (a *Admin) handleProviderUpsert(c *gin.Context) {
	var req providerReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Name) == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "code/name 必填")
		return
	}
	if req.Protocol == "" {
		req.Protocol = "openai_compat"
	}
	if req.AuthType == "" {
		req.AuthType = "bearer"
	}
	if req.Status == "" {
		req.Status = "active"
	}
	p := &model.Provider{Code: req.Code, Name: req.Name, Protocol: req.Protocol, BaseURL: req.BaseURL,
		AuthType: req.AuthType, Capabilities: req.Capabilities, Status: req.Status}
	if req.BillOnFailure != nil {
		p.BillOnFailure = *req.BillOnFailure
	}
	if err := a.Repo.UpsertProvider(c.Request.Context(), p); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "供应商保存失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "provider.upsert", TargetType: "provider", TargetID: p.Code}, "success")
	c.JSON(http.StatusOK, providerJSON(*p))
}

// ── 模型（07 §4.2）──────────────────────────────────────────

type modelReq struct {
	Name            string   `json:"name"`
	ProviderCode    string   `json:"provider_code"`
	DisplayName     string   `json:"display_name"`
	ContextWindow   int      `json:"context_window"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	Capabilities    []string `json:"capabilities"`
	BillingMode     string   `json:"billing_mode"`
	Fallbacks       []string `json:"fallbacks"`
	Status          string   `json:"status"`
}

// modelPatchReq 模型 PATCH：指针表达"未提供字段保持现值"，
// 杜绝非指针结构把缺省字段清零（如 PATCH {} 曾把 name/context_window 清空致模型损坏）。
type modelPatchReq struct {
	Name            *string   `json:"name"`
	ProviderCode    *string   `json:"provider_code"`
	DisplayName     *string   `json:"display_name"`
	ContextWindow   *int      `json:"context_window"`
	MaxOutputTokens *int      `json:"max_output_tokens"`
	Capabilities    *[]string `json:"capabilities"`
	BillingMode     *string   `json:"billing_mode"`
	Fallbacks       *[]string `json:"fallbacks"`
	Status          *string   `json:"status"`
}

func (r modelPatchReq) fields() map[string]any {
	m := map[string]any{}
	if r.Name != nil {
		m["name"] = *r.Name
	}
	if r.ProviderCode != nil {
		m["provider_code"] = *r.ProviderCode
	}
	if r.DisplayName != nil {
		m["display_name"] = *r.DisplayName
	}
	if r.ContextWindow != nil {
		m["context_window"] = *r.ContextWindow
	}
	if r.MaxOutputTokens != nil {
		m["max_output_tokens"] = *r.MaxOutputTokens
	}
	if r.Capabilities != nil {
		m["capabilities"] = *r.Capabilities
	}
	if r.BillingMode != nil {
		m["billing_mode"] = *r.BillingMode
	}
	if r.Fallbacks != nil {
		m["fallbacks"] = *r.Fallbacks
	}
	if r.Status != nil {
		m["status"] = *r.Status
	}
	return m
}

func modelJSON(m model.Model) gin.H {
	return gin.H{"id": m.ID, "name": m.Name, "provider_code": m.ProviderCode,
		"display_name": m.DisplayName, "context_window": m.ContextWindow,
		"max_output_tokens": m.MaxOutputTokens, "capabilities": m.Capabilities,
		"billing_mode": m.BillingMode, "fallbacks": m.Fallbacks, "status": m.Status}
}

func (a *Admin) handleModelsList(c *gin.Context) {
	ms, err := a.Repo.ListModels(c.Request.Context(), c.Query("provider_code"))
	if err != nil {
		writeAdminError(c, http.StatusInternalServerError, "server_error", "模型列表失败")
		return
	}
	items := make([]gin.H, 0, len(ms))
	for _, m := range ms {
		items = append(items, modelJSON(m))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *Admin) handleModelCreate(c *gin.Context) {
	var req modelReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.ProviderCode) == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "name/provider_code 必填")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.BillingMode == "" {
		req.BillingMode = "token"
	}
	m := &model.Model{Name: req.Name, ProviderCode: req.ProviderCode, DisplayName: req.DisplayName,
		ContextWindow: req.ContextWindow, MaxOutputTokens: req.MaxOutputTokens,
		Capabilities: req.Capabilities, BillingMode: req.BillingMode, Fallbacks: req.Fallbacks, Status: req.Status}
	if err := a.Repo.CreateModel(c.Request.Context(), m); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "模型名已存在")
			return
		}
		writeAdminError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "model.create", TargetType: "model", TargetID: idStr(m.ID)}, "success")
	c.JSON(http.StatusOK, modelJSON(*m))
}

func (a *Admin) handleModelPatch(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req modelPatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	fields := req.fields()
	if len(fields) == 0 {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "没有可更新的字段")
		return
	}
	// PATCH 语义：仅更新传入字段（指针结构），未提供字段保持现值——此前非指针结构会把 name/context 等清零。
	if err := a.Repo.UpdateModelFields(c.Request.Context(), id, fields); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "模型不存在")
			return
		}
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "模型名已存在")
			return
		}
		a.log().Error("admin model patch", "id", id, "error", err)
		writeAdminError(c, http.StatusInternalServerError, "server_error", "更新失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "model.update", TargetType: "model", TargetID: idStr(id)}, "success")
	c.Status(http.StatusOK)
}

func (a *Admin) handleModelDelete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := a.Repo.DeleteModel(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "模型不存在")
			return
		}
		writeAdminError(c, http.StatusInternalServerError, "server_error", "删除失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "model.delete", TargetType: "model", TargetID: idStr(id)}, "success")
	c.Status(http.StatusNoContent)
}

// ── 定价（07 §4.2，金额十进制文本 + effective_from）──────────

type priceReq struct {
	Currency        string  `json:"currency"`
	InputPer1K      string  `json:"input_price_per_1k"`
	OutputPer1K     string  `json:"output_price_per_1k"`
	CacheReadPer1K  *string `json:"cache_read_price_per_1k"`
	CacheWritePer1K *string `json:"cache_write_price_per_1k"`
	PerRequest      string  `json:"per_request_price"`
	EffectiveFrom   string  `json:"effective_from"` // "2006-01-02" 或 RFC3339
	EffectiveTo     *string `json:"effective_to"`
}

func (r priceReq) build(modelID int64) (*model.ModelPrice, error) {
	decOr0 := func(s string) (model.Decimal, error) {
		if strings.TrimSpace(s) == "" {
			return model.Decimal{}, nil
		}
		d, err := decimal.NewFromString(s)
		if err != nil {
			return model.Decimal{}, err
		}
		return model.Decimal{Decimal: d}, nil
	}
	p := &model.ModelPrice{ModelID: modelID, Currency: r.Currency}
	var err error
	if p.InputPricePer1K, err = decOr0(r.InputPer1K); err != nil {
		return nil, err
	}
	if p.OutputPricePer1K, err = decOr0(r.OutputPer1K); err != nil {
		return nil, err
	}
	if p.PerRequestPrice, err = decOr0(r.PerRequest); err != nil {
		return nil, err
	}
	if r.CacheReadPer1K != nil {
		d, err := decimal.NewFromString(*r.CacheReadPer1K)
		if err != nil {
			return nil, err
		}
		p.CacheReadPricePer1K = &model.Decimal{Decimal: d}
	}
	if r.CacheWritePer1K != nil {
		d, err := decimal.NewFromString(*r.CacheWritePer1K)
		if err != nil {
			return nil, err
		}
		p.CacheWritePricePer1K = &model.Decimal{Decimal: d}
	}
	if r.EffectiveFrom != "" {
		t, err := parseEffTime(r.EffectiveFrom)
		if err != nil {
			return nil, err
		}
		p.EffectiveFrom = t
	} else {
		p.EffectiveFrom = time.Now().UTC().Truncate(24 * time.Hour)
	}
	if r.EffectiveTo != nil {
		t, err := parseEffTime(*r.EffectiveTo)
		if err != nil {
			return nil, err
		}
		p.EffectiveTo = &t
	}
	if p.Currency == "" {
		p.Currency = "USD"
	}
	return p, nil
}

func parseEffTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("无效时间格式（支持 2006-01-02 或 RFC3339）")
}

func (a *Admin) handlePricesList(c *gin.Context) {
	mid, err := parseID64(c.Query("model_id"))
	if err != nil || mid <= 0 {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "model_id 必填")
		return
	}
	ps, err := a.Repo.ListModelPrices(c.Request.Context(), mid)
	if err != nil {
		writeAdminError(c, http.StatusInternalServerError, "server_error", "定价查询失败")
		return
	}
	items := make([]gin.H, 0, len(ps))
	for _, p := range ps {
		items = append(items, priceJSON(p))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func priceJSON(p model.ModelPrice) gin.H {
	return gin.H{"id": p.ID, "model_id": p.ModelID, "currency": p.Currency,
		"input_price_per_1k": p.InputPricePer1K.String(), "output_price_per_1k": p.OutputPricePer1K.String(),
		"per_request_price": p.PerRequestPrice.String(), "effective_from": p.EffectiveFrom,
		"effective_to": p.EffectiveTo}
}

func (a *Admin) handlePriceCreate(c *gin.Context) {
	mid, err := parseID64(c.Query("model_id"))
	if err != nil || mid <= 0 {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "model_id 必填")
		return
	}
	var req priceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	p, err := req.build(mid)
	if err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := a.Repo.CreateModelPrice(c.Request.Context(), p); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "该模型在该生效日已有价格")
			return
		}
		writeAdminError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "model_price.create", TargetType: "model_price", TargetID: idStr(p.ID)}, "success")
	c.JSON(http.StatusOK, priceJSON(*p))
}

// mergePriceReq PATCH 语义：未提供的字段继承现值（金额不会被缺省清 0、currency 不会回退 USD、
// effective_to/cache 保持现值）。显式空串金额 = 显式清零；cache 显式空串视为未提供（MVP 不支持单独清除）。
func mergePriceReq(r priceReq, cur *model.ModelPrice) priceReq {
	if r.Currency == "" {
		r.Currency = cur.Currency
	}
	if strings.TrimSpace(r.InputPer1K) == "" {
		r.InputPer1K = cur.InputPricePer1K.String()
	}
	if strings.TrimSpace(r.OutputPer1K) == "" {
		r.OutputPer1K = cur.OutputPricePer1K.String()
	}
	if strings.TrimSpace(r.PerRequest) == "" {
		r.PerRequest = cur.PerRequestPrice.String()
	}
	if (r.CacheReadPer1K == nil || strings.TrimSpace(*r.CacheReadPer1K) == "") && cur.CacheReadPricePer1K != nil {
		s := cur.CacheReadPricePer1K.String()
		r.CacheReadPer1K = &s
	}
	if (r.CacheWritePer1K == nil || strings.TrimSpace(*r.CacheWritePer1K) == "") && cur.CacheWritePricePer1K != nil {
		s := cur.CacheWritePricePer1K.String()
		r.CacheWritePer1K = &s
	}
	if strings.TrimSpace(r.EffectiveFrom) == "" {
		r.EffectiveFrom = cur.EffectiveFrom.Format(time.RFC3339)
	}
	return r
}

func (a *Admin) handlePricePatch(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req priceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "请求体非法")
		return
	}
	// 需要 modelID：查现有价格归属
	cur, err := a.Repo.PriceByID(c.Request.Context(), id)
	if err != nil || cur == nil {
		if cur == nil {
			writeAdminError(c, http.StatusNotFound, "not_found", "价格不存在")
			return
		}
		writeAdminError(c, http.StatusInternalServerError, "server_error", "查询失败")
		return
	}
	// PATCH 合并现值：未提供金额不得被清零（曾有 PATCH 只改有效日把 0.05 清成 0 白送流量）
	p, err := mergePriceReq(req, cur).build(cur.ModelID)
	if err != nil {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := a.Repo.UpdateModelPrice(c.Request.Context(), id, p); err != nil {
		if repository.IsUniqueViolation(err) {
			writeAdminError(c, http.StatusConflict, "conflict", "生效日冲突")
			return
		}
		writeAdminError(c, http.StatusInternalServerError, "server_error", "更新失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "model_price.update", TargetType: "model_price", TargetID: idStr(id)}, "success")
	c.Status(http.StatusOK)
}

func (a *Admin) handlePriceDelete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := a.Repo.DeleteModelPrice(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "价格不存在")
			return
		}
		writeAdminError(c, http.StatusInternalServerError, "server_error", "删除失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "model_price.delete", TargetType: "model_price", TargetID: idStr(id)}, "success")
	c.Status(http.StatusNoContent)
}

// ── 模型映射（07 §4.2）──────────────────────────────────────

type mappingReq struct {
	ChannelID     *int64 `json:"channel_id"`
	Alias         string `json:"alias"`
	UpstreamModel string `json:"upstream_model"`
	Priority      int    `json:"priority"`
}

func (a *Admin) handleMappingsList(c *gin.Context) {
	ms, err := a.Repo.ListModelMappings(c.Request.Context(), c.Query("alias"))
	if err != nil {
		writeAdminError(c, http.StatusInternalServerError, "server_error", "映射查询失败")
		return
	}
	items := make([]gin.H, 0, len(ms))
	for _, m := range ms {
		items = append(items, gin.H{"id": m.ID, "channel_id": m.ChannelID, "alias": m.Alias,
			"upstream_model": m.UpstreamModel, "priority": m.Priority})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *Admin) handleMappingCreate(c *gin.Context) {
	var req mappingReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Alias) == "" || strings.TrimSpace(req.UpstreamModel) == "" {
		writeAdminError(c, http.StatusBadRequest, "invalid_request", "alias/upstream_model 必填")
		return
	}
	m := &model.ModelMapping{ChannelID: req.ChannelID, Alias: req.Alias, UpstreamModel: req.UpstreamModel, Priority: req.Priority}
	if err := a.Repo.CreateModelMapping(c.Request.Context(), m); err != nil {
		writeAdminError(c, http.StatusInternalServerError, "server_error", "创建失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "model_mapping.create", TargetType: "model_mapping", TargetID: idStr(m.ID)}, "success")
	c.JSON(http.StatusOK, gin.H{"id": m.ID})
}

func (a *Admin) handleMappingDelete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := a.Repo.DeleteModelMapping(c.Request.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeAdminError(c, http.StatusNotFound, "not_found", "映射不存在")
			return
		}
		writeAdminError(c, http.StatusInternalServerError, "server_error", "删除失败")
		return
	}
	_ = a.audit(c, auditEntry{Action: "model_mapping.delete", TargetType: "model_mapping", TargetID: idStr(id)}, "success")
	c.Status(http.StatusNoContent)
}
