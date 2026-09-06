package model

import "time"

// Channel 渠道（02 §4.2：调度与容灾核心实体；§15.4 权威模型示例）。
// 索引名与迁移 SQL 一致（idx_channels_*）；部分唯一/部分索引（expires_at、deleted_at）在迁移 SQL 建。
type Channel struct {
	ID           int64   `gorm:"primaryKey;column:id"`
	Name         string  `gorm:"column:name;type:varchar(100);not null"`
	ProviderID   int64   `gorm:"column:provider_id;not null;index:idx_channels_provider"`                            // 外键 → providers.id（§4.2）
	ProviderCode string  `gorm:"column:provider_code;type:varchar(50);not null;index:idx_channels_sched,priority:1"` // 冗余，与 provider_id 同步写入
	BaseURL      *string `gorm:"column:base_url;type:varchar(512)"`
	// 可空 = 继承 providers.auth_type；非空取 bearer/header/oauth/signature
	AuthType *string `gorm:"column:auth_type;type:varchar(20)"`

	// 信封加密后的凭证密文，明文永不落库（08 §6）
	Credentials map[string]any `gorm:"column:credentials;type:jsonb;serializer:json;not null;default:'{}'"`
	CredKeyID   *string        `gorm:"column:cred_key_id;type:varchar(64)"`
	Extra       map[string]any `gorm:"column:extra;type:jsonb;serializer:json"`
	ProxyID     *int64         `gorm:"column:proxy_id"`

	// 调度（复合索引 idx_channels_sched：provider_code, priority, status）
	Priority    int `gorm:"column:priority;not null;default:50;index:idx_channels_sched,priority:2;comment:越小越优先"`
	Weight      int `gorm:"column:weight;not null;default:100"`
	Concurrency int `gorm:"column:concurrency;not null;default:3"`

	RateMultiplier Decimal `gorm:"column:rate_multiplier;type:numeric(10,4);not null;default:1"`
	// TokenRatio 本地 tokenizer 估算修正系数；ZeroUsageCount 上游 usage 全 0 累计（06 §11.2/§11.3）
	TokenRatio     Decimal `gorm:"column:token_ratio;type:numeric(10,4);not null;default:1"`
	ZeroUsageCount int     `gorm:"column:zero_usage_count;not null;default:0"`

	// 状态与健康
	Status           string     `gorm:"column:status;type:varchar(20);not null;default:'active';index:idx_channels_status_sched,priority:1;index:idx_channels_sched,priority:3"`
	Schedulable      bool       `gorm:"column:schedulable;not null;default:true;index:idx_channels_status_sched,priority:2"`
	ErrorMessage     *string    `gorm:"column:error_message;type:text"`
	HealthScore      int        `gorm:"column:health_score;not null;default:100;index:idx_channels_health_score"`
	CircuitState     string     `gorm:"column:circuit_state;type:varchar(20);not null;default:'closed';index:idx_channels_circuit,priority:1"`
	CircuitOpenUntil *time.Time `gorm:"column:circuit_open_until;type:timestamptz;index:idx_channels_circuit,priority:2"`
	CircuitFailCount int        `gorm:"column:circuit_fail_count;not null;default:0"`

	// 冷却
	RateLimitedAt           *time.Time `gorm:"column:rate_limited_at;type:timestamptz"`
	RateLimitResetAt        *time.Time `gorm:"column:rate_limit_reset_at;type:timestamptz;index:idx_channels_rate_limit_reset_at"`
	OverloadUntil           *time.Time `gorm:"column:overload_until;type:timestamptz"`
	TempUnschedulableUntil  *time.Time `gorm:"column:temp_unschedulable_until;type:timestamptz"`
	TempUnschedulableReason *string    `gorm:"column:temp_unschedulable_reason;type:text"`

	LastUsedAt         *time.Time `gorm:"column:last_used_at;type:timestamptz;index:idx_channels_last_used_at"`
	ExpiresAt          *time.Time `gorm:"column:expires_at;type:timestamptz"`
	AutoPauseOnExpired bool       `gorm:"column:auto_pause_on_expired;not null;default:true"`

	// 溯源（02 §16：业务实体表统一字段）
	Source   string `gorm:"column:source;type:varchar(20);not null;default:'user'"` // builtin | migration | user
	SourceID string `gorm:"column:source_id;type:varchar(128)"`                     // 仅 source='migration' 时有值

	Timestamps // created_at / updated_at（§15.2）
	SoftDelete // deleted_at；部分唯一索引由迁移 SQL 负责（§10.4）
}

func (Channel) TableName() string { return "channels" }
