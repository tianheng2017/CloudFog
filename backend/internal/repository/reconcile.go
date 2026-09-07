package repository

import (
	"context"
	"time"

	"cloudfog/internal/model"
)

// DayReconcile 单日对账结果（06 §7/§10：usage 应计成本 ↔ settle 已扣流水）。
type DayReconcile struct {
	UsageTotal   model.Decimal // 当日 usage_logs.sum(total_cost)（正）
	SettledTotal model.Decimal // 当日 type='settle' 流水扣费绝对值
	Diff         model.Decimal // usageTotal - settledTotal（>0 应计未扣/漏结算；<0 多扣）
	UsageCount   int64
	SettleCount  int64
}

// ReconcileDay 比对某 UTC 日 usage_logs 应计成本与 billing_ledger settle 扣费。
// 口径：settle 流水 amount 为负（扣减），对账取其绝对值；diff≈0 即一致。
// 仅统计不落库不改账（差异由调用方记录/告警，06 §7 差异进人工队列）。
func (r *Repository) ReconcileDay(ctx context.Context, day time.Time) (*DayReconcile, error) {
	day = day.UTC().Truncate(24 * time.Hour)
	next := day.Add(24 * time.Hour)

	var usage struct {
		UsageTotal model.Decimal `gorm:"column:usage_total"`
		UsageCount int64         `gorm:"column:usage_count"`
	}
	if err := r.db.WithContext(ctx).Model(&model.UsageLog{}).
		Select("coalesce(sum(total_cost),0) AS usage_total, count(*) AS usage_count").
		Where("created_at >= ? AND created_at < ?", day, next).
		Scan(&usage).Error; err != nil {
		return nil, err
	}
	var settled struct {
		SettledTotal model.Decimal `gorm:"column:settled_total"`
		SettleCount  int64         `gorm:"column:settle_count"`
	}
	if err := r.db.WithContext(ctx).Model(&model.BillingLedger{}).
		Select("coalesce(sum(amount),0) AS settled_total, count(*) AS settle_count").
		Where("type = 'settle' AND created_at >= ? AND created_at < ?", day, next).
		Scan(&settled).Error; err != nil {
		return nil, err
	}

	dr := &DayReconcile{
		UsageTotal:   usage.UsageTotal,
		UsageCount:   usage.UsageCount,
		SettleCount:  settled.SettleCount,
		SettledTotal: model.Decimal{Decimal: settled.SettledTotal.Abs()},
	}
	dr.Diff = model.Decimal{Decimal: dr.UsageTotal.Sub(dr.SettledTotal.Decimal)}
	return dr, nil
}
