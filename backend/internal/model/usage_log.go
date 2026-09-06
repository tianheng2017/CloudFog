package model

import "time"

// UsageLog 调用日志（02 §6.1：核心大表，只追加；§15.4 权威模型示例）。
// 注意：① 无 SoftDelete（只追加）② 只有 created_at（无 updated_at，等价 Immutable）
//
//	③ 物理结构按月分区，建表与分区由迁移 SQL 手工维护（§15.5），模型只做读写映射。
//
// 禁止 UPDATE / DELETE（DB 权限 + 触发器双重保证，迁移 SQL 建）。
type UsageLog struct {
	ID             int64  `gorm:"primaryKey;column:id"` // 分区表主键实为 (id, created_at)，见 §15.5
	RequestID      string `gorm:"column:request_id;type:varchar(64);not null;index:idx_usage_logs_request_id"`
	UserID         int64  `gorm:"column:user_id;not null;index:idx_usage_logs_user_created,priority:1"`
	APIKeyID       int64  `gorm:"column:api_key_id;not null;index:idx_usage_logs_key_created,priority:1"`
	ChannelID      int64  `gorm:"column:channel_id;not null"`
	GroupID        *int64 `gorm:"column:group_id;index:idx_usage_logs_group_created,priority:1"`
	SubscriptionID *int64 `gorm:"column:subscription_id"`

	Model         string `gorm:"column:model;type:varchar(100);not null;index:idx_usage_logs_model"`
	UpstreamModel string `gorm:"column:upstream_model;type:varchar(100)"`
	ProviderCode  string `gorm:"column:provider_code;type:varchar(50);not null"`
	MappingChain  string `gorm:"column:mapping_chain;type:varchar(500)"`

	InputTokens      int    `gorm:"column:input_tokens;not null;default:0"`
	OutputTokens     int    `gorm:"column:output_tokens;not null;default:0"`
	CacheReadTokens  int    `gorm:"column:cache_read_tokens;not null;default:0"`
	CacheWriteTokens int    `gorm:"column:cache_write_tokens;not null;default:0"`
	CacheWriteTTL    string `gorm:"column:cache_write_ttl;type:varchar(8)"`
	ImageCount       int    `gorm:"column:image_count;not null;default:0"`
	RequestCount     int    `gorm:"column:request_count;not null;default:0"`

	// 金额：中间分项保持高精度，total_cost 为已舍入值（§14.2）
	InputCost      Decimal `gorm:"column:input_cost;type:numeric(20,10);not null;default:0"`
	OutputCost     Decimal `gorm:"column:output_cost;type:numeric(20,10);not null;default:0"`
	CacheReadCost  Decimal `gorm:"column:cache_read_cost;type:numeric(20,10);not null;default:0"`
	CacheWriteCost Decimal `gorm:"column:cache_write_cost;type:numeric(20,10);not null;default:0"`
	TotalCost      Decimal `gorm:"column:total_cost;type:numeric(20,10);not null;default:0"`
	UpstreamCost   Decimal `gorm:"column:upstream_cost;type:numeric(20,10);not null;default:0"`

	RateMultiplier    Decimal        `gorm:"column:rate_multiplier;type:numeric(10,4);not null;default:1"`
	ChannelMultiplier Decimal        `gorm:"column:channel_multiplier;type:numeric(10,4);not null;default:1"`
	PriceSnapshot     map[string]any `gorm:"column:price_snapshot;type:jsonb;serializer:json"`

	BillingMode string `gorm:"column:billing_mode;type:varchar(20);not null;default:'token'"`
	UsageSource string `gorm:"column:usage_source;type:varchar(20);not null;default:'upstream'"` // upstream | partial | estimated

	Stream       bool   `gorm:"column:stream;not null;default:false"`
	DurationMs   *int   `gorm:"column:duration_ms"`
	FirstTokenMs *int   `gorm:"column:first_token_ms"`
	StatusCode   int    `gorm:"column:status_code;not null;default:200"`
	ErrorCode    string `gorm:"column:error_code;type:varchar(64)"`
	SwitchCount  int    `gorm:"column:switch_count;not null;default:0"`
	Degraded     bool   `gorm:"column:degraded;not null;default:false"`
	RetryCount   int    `gorm:"column:retry_count;not null;default:0"`

	ClientIP  string `gorm:"column:client_ip;type:varchar(45)"`
	UserAgent string `gorm:"column:user_agent;type:varchar(512)"`

	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;autoCreateTime;index:idx_usage_logs_user_created,priority:2;index:idx_usage_logs_key_created,priority:2;index:idx_usage_logs_group_created,priority:2"`
}

func (UsageLog) TableName() string { return "usage_logs" }
