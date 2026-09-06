package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"cloudfog/internal/model"
)

// GroupPatch 管理端分组可改字段（指针 = 显式意图，零值语义交由调用方）。
type GroupPatch struct {
	Name             *string
	Status           *string
	RateMultiplier   *decimal.Decimal
	AllowedModels    *[]string
	FallbackModels   *[]string
	RPMLimit         *int
	ConcurrencyLimit *int
	DailyQuotaUSD    *decimal.Decimal
	SortOrder        *int
}

// Apply 组装 updates（jsonb 数组序列化为 RawMessage 保证 GORM 写入 PG jsonb）。
func (p GroupPatch) Apply() (map[string]any, error) {
	m := map[string]any{}
	if p.Name != nil {
		if *p.Name == "" {
			return nil, errors.New("repository: 分组名不能为空")
		}
		m["name"] = *p.Name
	}
	if p.Status != nil {
		m["status"] = *p.Status
	}
	if p.RateMultiplier != nil {
		m["rate_multiplier"] = p.RateMultiplier.String()
	}
	if p.AllowedModels != nil {
		b, err := json.Marshal(*p.AllowedModels)
		if err != nil {
			return nil, err
		}
		m["allowed_models"] = json.RawMessage(b)
	}
	if p.FallbackModels != nil {
		b, err := json.Marshal(*p.FallbackModels)
		if err != nil {
			return nil, err
		}
		m["fallback_models"] = json.RawMessage(b)
	}
	if p.RPMLimit != nil {
		if *p.RPMLimit < 0 {
			return nil, errors.New("repository: rpm_limit 不能为负")
		}
		m["rpm_limit"] = *p.RPMLimit
	}
	if p.ConcurrencyLimit != nil {
		if *p.ConcurrencyLimit < 0 {
			return nil, errors.New("repository: concurrency_limit 不能为负")
		}
		m["concurrency_limit"] = *p.ConcurrencyLimit
	}
	if p.DailyQuotaUSD != nil {
		m["daily_quota_usd"] = p.DailyQuotaUSD.String()
	}
	if p.SortOrder != nil {
		m["sort_order"] = *p.SortOrder
	}
	return m, nil
}

// CreateGroup 创建分组（默认 status=active、rate=1）。
func (r *Repository) CreateGroup(ctx context.Context, g *model.Group) error {
	if g.RateMultiplier.IsZero() {
		g.RateMultiplier = model.Decimal{Decimal: decimal.NewFromInt(1)}
	}
	if g.Status == "" {
		g.Status = "active"
	}
	return r.db.WithContext(ctx).Create(g).Error
}

// UpdateGroup 更新分组。
func (r *Repository) UpdateGroup(ctx context.Context, id int64, p GroupPatch) error {
	m, err := p.Apply()
	if err != nil {
		return err
	}
	if len(m) == 0 {
		return nil
	}
	m["updated_at"] = time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&model.Group{}).Where("id = ?", id).Updates(m)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteGroup 软删分组（引用该组的关系受 DB 外键/业务约束；失败即不可删）。
func (r *Repository) DeleteGroup(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Group{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UserAllowedGroupIDs 用户可访问分组 id 列表。
func (r *Repository) UserAllowedGroupIDs(ctx context.Context, userID int64) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Model(&model.UserAllowedGroup{}).
		Where("user_id = ?", userID).Order("group_id ASC").Pluck("group_id", &ids).Error
	return ids, err
}

// ChannelGroupIDs 分组可见渠道 id 列表（管理查看用）。
func (r *Repository) ChannelGroupIDs(ctx context.Context, groupID int64) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Model(&model.ChannelGroup{}).
		Where("group_id = ?", groupID).Order("channel_id ASC").Pluck("channel_id", &ids).Error
	return ids, err
}
