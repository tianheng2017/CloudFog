package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"cloudfog/internal/model"
)

// AggregateUsageDay 把某 UTC 日 usage_logs 按 (user_id, model) 聚合写入 usage_daily_stats。
// MVP 维度：user × model（api_key_id/channel_id 留 NULL；看板批次扩展维度时按唯一键粒度 upsert）。
// 幂等：整日 delete + insert（仅统计表非账本；同日重复执行结果一致）。
func (r *Repository) AggregateUsageDay(ctx context.Context, day time.Time) (int, error) {
	day = day.UTC().Truncate(24 * time.Hour)
	next := day.Add(24 * time.Hour)
	rows, err := aggregateUsageRows(ctx, r.db, day, next)
	if err != nil {
		return 0, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Where("stat_date = ?", day).Delete(&model.UsageDailyStat{}).Error; e != nil {
			return e
		}
		for _, row := range rows {
			if e := tx.Create(row).Error; e != nil {
				return e
			}
		}
		return nil
	})
	return len(rows), err
}

// aggregateUsageRows 按 (user_id, model) 汇总日区间 usage_logs。
func aggregateUsageRows(ctx context.Context, db *gorm.DB, from, to time.Time) ([]*model.UsageDailyStat, error) {
	type raw struct {
		UserID       int64
		Model        string
		RequestCount int64
		SuccessCount int64
		InputTokens  int64
		OutputTokens int64
		CacheRead    int64
		CacheWrite   int64
		TotalCost    model.Decimal
		UpstreamCost model.Decimal
	}
	var rs []raw
	err := db.WithContext(ctx).Model(&model.UsageLog{}).
		Select("user_id, model, count(*) AS request_count, "+
			"count(*) FILTER (WHERE status_code >= 200 AND status_code < 400 AND coalesce(error_code,'') = '') AS success_count, "+
			"coalesce(sum(input_tokens),0) AS input_tokens, coalesce(sum(output_tokens),0) AS output_tokens, "+
			"coalesce(sum(cache_read_tokens),0) AS cache_read, coalesce(sum(cache_write_tokens),0) AS cache_write, "+
			"coalesce(sum(total_cost),0) AS total_cost, coalesce(sum(upstream_cost),0) AS upstream_cost").
		Where("created_at >= ? AND created_at < ?", from, to).
		Group("user_id, model").
		Scan(&rs).Error
	if err != nil {
		return nil, err
	}
	out := make([]*model.UsageDailyStat, 0, len(rs))
	for _, x := range rs {
		m := x.Model
		stat := &model.UsageDailyStat{
			StatDate: from, UserID: x.UserID, Model: &m,
			RequestCount: x.RequestCount, SuccessCount: x.SuccessCount,
			ErrorCount:  x.RequestCount - x.SuccessCount,
			InputTokens: x.InputTokens, OutputTokens: x.OutputTokens,
			CacheReadTokens: x.CacheRead, CacheWriteTokens: x.CacheWrite,
			TotalCost: x.TotalCost, UpstreamCost: x.UpstreamCost,
		}
		out = append(out, stat)
	}
	return out, nil
}

// UsageTotals 用户某时段用量汇总（/me/summary：今日/本月，07 §3.1）。
type UsageTotals struct {
	Requests     int64
	InputTokens  int64
	OutputTokens int64
	TotalCost    model.Decimal
}

func (r *Repository) UsageTotals(ctx context.Context, userID int64, from, to time.Time) (UsageTotals, error) {
	var t UsageTotals
	err := r.db.WithContext(ctx).Model(&model.UsageLog{}).
		Select("count(*) AS requests, coalesce(sum(input_tokens),0) AS input_tokens, "+
			"coalesce(sum(output_tokens),0) AS output_tokens, coalesce(sum(total_cost),0) AS total_cost").
		Where("user_id = ? AND created_at >= ? AND created_at < ?", userID, from, to).
		Scan(&t).Error
	return t, err
}
