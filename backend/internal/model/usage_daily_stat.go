package model

import "time"

// UsageDailyStat 天级聚合（02 §6.2）：看板不直接扫 usage_logs。
// stat_date = 北京日历日（date 列，2026-09-07 起口径为北京时间；内部聚合与用户统计统一）。
// 唯一键 (stat_date, user_id, model, api_key_id)（空值占位允许 NULL）。
// p95 用 jsonb 直方图桶计数存储（§6.2 例外 1）；avg_* 的"总和+次数"由 stats:aggregate 任务控制，
// 本模型只承载 sum 值；展示均值时应用层相除。
type UsageDailyStat struct {
	ID        int64     `gorm:"primaryKey;column:id"`
	StatDate  time.Time `gorm:"column:stat_date;type:date;not null"`
	UserID    int64     `gorm:"column:user_id;not null"`
	APIKeyID  *int64    `gorm:"column:api_key_id"`              // 空 = 用户维度汇总
	Model     *string   `gorm:"column:model;type:varchar(100)"` // 空 = 模型维度汇总
	ChannelID *int64    `gorm:"column:channel_id"`

	RequestCount     int64 `gorm:"column:request_count;not null;default:0"`
	SuccessCount     int64 `gorm:"column:success_count;not null;default:0"`
	ErrorCount       int64 `gorm:"column:error_count;not null;default:0"`
	InputTokens      int64 `gorm:"column:input_tokens;not null;default:0"`
	OutputTokens     int64 `gorm:"column:output_tokens;not null;default:0"`
	CacheReadTokens  int64 `gorm:"column:cache_read_tokens;not null;default:0"`
	CacheWriteTokens int64 `gorm:"column:cache_write_tokens;not null;default:0"`

	TotalCost    Decimal `gorm:"column:total_cost;type:numeric(20,10);not null;default:0"`
	UpstreamCost Decimal `gorm:"column:upstream_cost;type:numeric(20,10);not null;default:0"`

	AvgDurationMs     int            `gorm:"column:avg_duration_ms;not null;default:0"`
	AvgFirstTokenMs   int            `gorm:"column:avg_first_token_ms;not null;default:0"`
	P95DurationMs     int            `gorm:"column:p95_duration_ms;not null;default:0"`
	DurationHistogram map[string]int `gorm:"column:duration_histogram;type:jsonb;serializer:json"` // bucket→count（§6.2 推荐 ①）

	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;autoUpdateTime"`
}

func (UsageDailyStat) TableName() string { return "usage_daily_stats" }
