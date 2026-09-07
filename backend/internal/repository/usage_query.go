package repository

import (
	"context"
	"time"

	"cloudfog/internal/model"
)

// UsageLogQuery 用户用量明细分页筛选（07 §3.1 /me/usage）。
type UsageLogQuery struct {
	UserID int64
	From   *time.Time
	To     *time.Time
	Offset int
	Limit  int
}

const maxQueryLimit = 100

// ListUsageLogs 用量明细（按 created_at 倒序分页）。limit 保护 + 兜底默认 20。
func (r *Repository) ListUsageLogs(ctx context.Context, q UsageLogQuery) ([]model.UsageLog, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.UsageLog{}).Where("user_id = ?", q.UserID)
	if q.From != nil {
		db = db.Where("created_at >= ?", *q.From)
	}
	if q.To != nil {
		db = db.Where("created_at < ?", *q.To)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > maxQueryLimit {
		q.Limit = maxQueryLimit
	}
	var rows []model.UsageLog
	err := db.Order("created_at DESC, id DESC").Offset(q.Offset).Limit(q.Limit).Find(&rows).Error
	return rows, total, err
}

// UsageStatRow 单模型用量聚合行。
type UsageStatRow struct {
	Model            string        `gorm:"column:model"`
	Requests         int64         `gorm:"column:requests"`
	InputTokens      int64         `gorm:"column:input_tokens"`
	OutputTokens     int64         `gorm:"column:output_tokens"`
	CacheReadTokens  int64         `gorm:"column:cache_read_tokens"`
	CacheWriteTokens int64         `gorm:"column:cache_write_tokens"`
	TotalCost        model.Decimal `gorm:"column:total_cost"`
}

// UsageAggregate 按模型聚合用量与金额（07 §3.1 /me/usage/stats）。
// 注：created_at 为 UTC；按天切分以 UTC 日界为准（平台时区账期归看板批次）。
func (r *Repository) UsageAggregate(ctx context.Context, userID int64, from, to *time.Time) ([]UsageStatRow, error) {
	db := r.db.WithContext(ctx).Model(&model.UsageLog{}).
		Select("model, count(*) AS requests, coalesce(sum(input_tokens),0) AS input_tokens, "+
			"coalesce(sum(output_tokens),0) AS output_tokens, coalesce(sum(cache_read_tokens),0) AS cache_read_tokens, "+
			"coalesce(sum(cache_write_tokens),0) AS cache_write_tokens, coalesce(sum(total_cost),0) AS total_cost").
		Where("user_id = ?", userID)
	if from != nil {
		db = db.Where("created_at >= ?", *from)
	}
	if to != nil {
		db = db.Where("created_at < ?", *to)
	}
	var rows []UsageStatRow
	err := db.Group("model").Order("requests DESC").Find(&rows).Error
	return rows, err
}

// ListLedgerByUser 账单流水（07 §3.1 /me/billing）。typeFilter 空=全部，否则按 type 过滤。
func (r *Repository) ListLedgerByUser(ctx context.Context, userID int64, typeFilter string, offset, limit int) ([]model.BillingLedger, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.BillingLedger{}).Where("user_id = ?", userID)
	if typeFilter != "" {
		db = db.Where("type = ?", typeFilter)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	var rows []model.BillingLedger
	err := db.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}
