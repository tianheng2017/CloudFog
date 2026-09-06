package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"cloudfog/internal/model"
)

// ── 模型目录读取（B2-3 路由/计价消费；02 §5）────────────────

// ModelByName 按对外模型名查规格（name 唯一部分索引）。未命中返回 (nil, nil)。
func (r *Repository) ModelByName(ctx context.Context, name string) (*model.Model, error) {
	var m model.Model
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CurrentModelPrice 取某模型在 at 时刻生效的价格（effective_from <= at，取最近生效）。
// 无生效价返回 (nil, nil)（调用方按 0 价/策略处理）。价格是历史事实表，不软删。
func (r *Repository) CurrentModelPrice(ctx context.Context, modelID int64, at time.Time) (*model.ModelPrice, error) {
	var p model.ModelPrice
	err := r.db.WithContext(ctx).
		Where("model_id = ? AND effective_from <= ?", modelID, at).
		Order("effective_from DESC").
		First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListActiveModels 全部对外可见模型（status='active'），按 sort_order 升序稳定输出。
// GET /v1/models 消费（B2-5）。
func (r *Repository) ListActiveModels(ctx context.Context) ([]model.Model, error) {
	var list []model.Model
	err := r.db.WithContext(ctx).
		Where("status = ?", "active").
		Order("sort_order ASC, id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// MappingRows 查某别名的全部模型映射（含渠道级与全局，B2-3 解析用）。
// 解析顺序（02 §5.3）：渠道级精确 → 渠道级通配 → 全局精确 → 全局通配 → 原样透传。
// 本方法返回候选行，渠道级/全局/精确/通配的选择逻辑在路由层完成。
func (r *Repository) MappingRows(ctx context.Context, alias string) ([]model.ModelMapping, error) {
	var rows []model.ModelMapping
	// 确定性：同一 (channel/alias) 命中多行时按 priority 升序取最优，id 做稳定 tie-break。
	err := r.db.WithContext(ctx).
		Where("alias = ?", alias).
		Order("priority ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
