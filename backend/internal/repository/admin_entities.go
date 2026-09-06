package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudfog/internal/model"
)

// ── 供应商（07 §4.2）────────────────────────────────────────

// ListProviders 全部启用供应商（管理列表）。
func (r *Repository) ListProviders(ctx context.Context, includeDisabled bool) ([]model.Provider, error) {
	q := r.db.WithContext(ctx).Model(&model.Provider{})
	if !includeDisabled {
		q = q.Where("status = 'active'")
	}
	var ps []model.Provider
	if err := q.Order("id ASC").Find(&ps).Error; err != nil {
		return nil, err
	}
	return ps, nil
}

// ProviderByCode 按 code 查（未命中 nil）。
func (r *Repository) ProviderByCode(ctx context.Context, code string) (*model.Provider, error) {
	var p model.Provider
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertProvider 创建或更新供应商元数据（code 唯一；存在则更新名称/能力/状态等）。
func (r *Repository) UpsertProvider(ctx context.Context, p *model.Provider) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "base_url", "protocol", "auth_type", "capabilities", "status", "updated_at"}),
	}).Create(p).Error
}

// ── 模型（07 §4.2）──────────────────────────────────────────

// ListModels 模型列表（可选含已删除外全部）。
func (r *Repository) ListModels(ctx context.Context, providerCode string) ([]model.Model, error) {
	q := r.db.WithContext(ctx).Model(&model.Model{})
	if providerCode != "" {
		q = q.Where("provider_code = ?", providerCode)
	}
	var ms []model.Model
	if err := q.Order("sort_order ASC, id ASC").Find(&ms).Error; err != nil {
		return nil, err
	}
	return ms, nil
}

// CreateModel 创建模型（name 部分唯一）。
func (r *Repository) CreateModel(ctx context.Context, m *model.Model) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// UpdateModel 更新模型基础字段（含 fallbacks/capabilities jsonb）。
func (r *Repository) UpdateModel(ctx context.Context, id int64, m *model.Model) error {
	m.ID = id
	m.UpdatedAt = time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&model.Model{}).Select(
		"name", "provider_code", "display_name", "context_window", "max_output_tokens",
		"capabilities", "billing_mode", "fallbacks", "status", "sort_order", "updated_at").
		Where("id = ?", id).Updates(m)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteModel 软删模型。
func (r *Repository) DeleteModel(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Model{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ── 模型映射（07 §4.2）─────────────────────────────────────

func (r *Repository) ListModelMappings(ctx context.Context, alias string) ([]model.ModelMapping, error) {
	q := r.db.WithContext(ctx).Model(&model.ModelMapping{})
	if alias != "" {
		q = q.Where("alias = ?", alias)
	}
	var ms []model.ModelMapping
	if err := q.Order("channel_id NULLS FIRST, priority DESC, id ASC").Find(&ms).Error; err != nil {
		return nil, err
	}
	return ms, nil
}

// CreateModelMapping 新增映射（无组合唯一约束；运维经列表去重，重复全局映射解析取首条）。
func (r *Repository) CreateModelMapping(ctx context.Context, m *model.ModelMapping) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// DeleteModelMapping 按 id 删。
func (r *Repository) DeleteModelMapping(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.ModelMapping{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ── 定价（07 §4.2，带生效时间，历史不软删）──────────────────

// PriceByID 按主键查价格条目（未命中 nil）。
func (r *Repository) PriceByID(ctx context.Context, id int64) (*model.ModelPrice, error) {
	var p model.ModelPrice
	err := r.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) ListModelPrices(ctx context.Context, modelID int64) ([]model.ModelPrice, error) {
	var ps []model.ModelPrice
	if err := r.db.WithContext(ctx).Where("model_id = ?", modelID).Order("effective_from DESC").Find(&ps).Error; err != nil {
		return nil, err
	}
	return ps, nil
}

// CreateModelPrice 新增生效价（(model_id,effective_from) 唯一冲突返回唯一错误由调用方映射 409）。
func (r *Repository) CreateModelPrice(ctx context.Context, p *model.ModelPrice) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// UpdateModelPrice 覆盖价格条目（目标不存在 → ErrRecordNotFound，供 handler 映射 404）。
func (r *Repository) UpdateModelPrice(ctx context.Context, id int64, p *model.ModelPrice) error {
	p.ID = id
	p.UpdatedAt = time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&model.ModelPrice{}).Select(
		"currency", "input_price_per_1k", "output_price_per_1k", "cache_read_price_per_1k",
		"cache_write_price_per_1k", "long_context_threshold", "long_context_input_price_per_1k",
		"long_context_output_price_per_1k", "per_request_price", "effective_from", "effective_to", "updated_at").
		Where("id = ?", id).Updates(p)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteModelPrice 删除价目（历史事实慎删；仅错误录入可删）。
func (r *Repository) DeleteModelPrice(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.ModelPrice{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ── 渠道（07 §4.2）──────────────────────────────────────────

// ChannelSealer 渠道凭证密封回调：在拿到自增 id 后于同一事务内加密（handler 注入 MK+AAD=channel:<id>），
// 使明文仅存在于内存，杜绝明文落库窗口。
type ChannelSealer func(channelID int64, blob map[string]any) (map[string]any, error)

// ChannelListFilter 渠道列表筛选。
type ChannelListFilter struct {
	ProviderCode string
	Status       string
	Name         string
	GroupID      *int64
	Offset       int
	Limit        int
}

func (r *Repository) ListChannels(ctx context.Context, f ChannelListFilter) ([]model.Channel, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Channel{})
	if f.ProviderCode != "" {
		q = q.Where("provider_code = ?", f.ProviderCode)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if s := strings.TrimSpace(f.Name); s != "" {
		q = q.Where("name ILIKE ?", "%"+s+"%")
	}
	if f.GroupID != nil {
		q = q.Where("id IN (SELECT channel_id FROM channel_groups WHERE group_id = ?)", *f.GroupID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	var cs []model.Channel
	// 列表 Omit credentials：密文不出 service 边界（即便 handler 忘脱敏也不外泄）；内部按需用 ChannelByID
	if err := q.Omit("credentials").Order("priority ASC, id ASC").Offset(maxInt(f.Offset, 0)).Limit(f.Limit).Find(&cs).Error; err != nil {
		return nil, 0, err
	}
	return cs, total, nil
}

func (r *Repository) ChannelByID(ctx context.Context, id int64) (*model.Channel, error) {
	var c model.Channel
	err := r.db.WithContext(ctx).First(&c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateChannel 创建渠道。credential 落库策略：
//   - seal 非 nil：先以空 credentials 插入拿到 id → 事务内回调 seal(id, blob) → 回写密文（明文零落库窗口）；
//   - seal nil：直接用 ch.Credentials（legacy dev 明文路径，测试/演示）。
func (r *Repository) CreateChannel(ctx context.Context, ch *model.Channel, seal ChannelSealer) (int64, error) {
	id := int64(0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c := *ch
		if seal != nil {
			c.Credentials = map[string]any{}
		}
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		id = c.ID
		if seal == nil {
			return nil
		}
		blob, err := seal(c.ID, ch.Credentials)
		if err != nil {
			return err
		}
		updates := map[string]any{"credentials": blob}
		if k, ok := blob["key_id"].(string); ok && k != "" {
			updates["cred_key_id"] = k
		}
		return tx.Model(&model.Channel{}).Where("id = ?", c.ID).Updates(updates).Error
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ChannelPatch 渠道可编辑基础字段（不涉凭证）。
type ChannelPatch struct {
	Name               *string
	ProviderCode       *string
	ProviderID         *int64
	BaseURL            *string
	AuthType           *string
	Priority           *int
	Weight             *int
	Concurrency        *int
	RateMultiplier     *string // decimal 文本
	TokenRatio         *string
	Schedulable        *bool
	Status             *string
	ExpiresAt          *time.Time
	AutoPauseOnExpired *bool
	ProxyID            *int64
}

func (p ChannelPatch) Apply() (map[string]any, error) {
	m := map[string]any{}
	if p.Name != nil {
		m["name"] = *p.Name
	}
	if p.ProviderCode != nil {
		m["provider_code"] = *p.ProviderCode
	}
	if p.ProviderID != nil {
		m["provider_id"] = *p.ProviderID
	}
	if p.BaseURL != nil {
		m["base_url"] = *p.BaseURL
	}
	if p.AuthType != nil {
		m["auth_type"] = *p.AuthType
	}
	if p.Priority != nil {
		m["priority"] = *p.Priority
	}
	if p.Weight != nil {
		m["weight"] = *p.Weight
	}
	if p.Concurrency != nil {
		m["concurrency"] = *p.Concurrency
	}
	if p.RateMultiplier != nil {
		m["rate_multiplier"] = *p.RateMultiplier
	}
	if p.TokenRatio != nil {
		m["token_ratio"] = *p.TokenRatio
	}
	if p.Schedulable != nil {
		m["schedulable"] = *p.Schedulable
	}
	if p.Status != nil {
		m["status"] = *p.Status
	}
	if p.ExpiresAt != nil {
		m["expires_at"] = *p.ExpiresAt
	}
	if p.AutoPauseOnExpired != nil {
		m["auto_pause_on_expired"] = *p.AutoPauseOnExpired
	}
	if p.ProxyID != nil {
		m["proxy_id"] = *p.ProxyID
	}
	// status ↔ schedulable 一致性：非 active 一律停排；恢复 active 且未显式给 Schedulable → 可调度。
	// 否则会产生 "status=active 但 schedulable=false" 的永不调度陷阱（界面误导 + 调度零命中）。
	if p.Status != nil && *p.Status != "active" {
		m["schedulable"] = false
	} else if p.Status != nil && p.Schedulable == nil {
		m["schedulable"] = true
	}
	if len(m) == 0 {
		return nil, errors.New("repository: 没有可更新的渠道字段")
	}
	return m, nil
}

func (r *Repository) UpdateChannel(ctx context.Context, id int64, p ChannelPatch) error {
	m, err := p.Apply()
	if err != nil {
		return err
	}
	m["updated_at"] = time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Updates(m)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SetChannelCredentials 凭证轮换：同事务重 seal（明文仅内存）。
func (r *Repository) SetChannelCredentials(ctx context.Context, id int64, seal ChannelSealer, plain map[string]any) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		blob, err := seal(id, plain)
		if err != nil {
			return err
		}
		updates := map[string]any{"credentials": blob, "updated_at": time.Now().UTC()}
		if k, ok := blob["key_id"].(string); ok && k != "" {
			updates["cred_key_id"] = k
		}
		res := tx.Model(&model.Channel{}).Where("id = ?", id).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// SetChannelEnabled 启停：disable → status=disabled 且停排；enable → active 且恢复可调度。
func (r *Repository) SetChannelEnabled(ctx context.Context, id int64, enabled bool) error {
	m := map[string]any{"updated_at": time.Now().UTC()}
	if enabled {
		m["status"] = "active"
		m["schedulable"] = true
	} else {
		m["status"] = "disabled"
		m["schedulable"] = false
	}
	res := r.db.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Updates(m)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ResetChannelCircuit 手动重置熔断：closed + 清冷却窗口 + 失败计数清零（07 §4.2 circuit/reset）。
func (r *Repository) ResetChannelCircuit(ctx context.Context, id int64) error {
	// GORM map 更新跳过 nil 值 → 可空字段复位须用 Expr("NULL")（否则冷却窗口永不消除、熔断复位失效）
	res := r.db.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Updates(map[string]any{
		"circuit_state":            "closed",
		"circuit_open_until":       gorm.Expr("NULL"),
		"circuit_fail_count":       0,
		"rate_limit_reset_at":      gorm.Expr("NULL"),
		"overload_until":           gorm.Expr("NULL"),
		"temp_unschedulable_until": gorm.Expr("NULL"),
		"error_message":            gorm.Expr("NULL"),
		"updated_at":               time.Now().UTC(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteChannel 软删渠道：先解绑 channel_groups（FK）再软删；返回 ErrRecordNotFound。
func (r *Repository) DeleteChannel(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("channel_id = ?", id).Delete(&model.ChannelGroup{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&model.Channel{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// ReplaceChannelGroups 全量替换渠道可见分组（幂等）。
func (r *Repository) ReplaceChannelGroups(ctx context.Context, channelID int64, groupIDs []int64) error {
	seen := map[int64]bool{}
	var uniq []int64
	for _, g := range groupIDs {
		if !seen[g] {
			seen[g] = true
			uniq = append(uniq, g)
		}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("channel_id = ?", channelID).Delete(&model.ChannelGroup{}).Error; err != nil {
			return err
		}
		for _, g := range uniq {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&model.ChannelGroup{ChannelID: channelID, GroupID: g}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
