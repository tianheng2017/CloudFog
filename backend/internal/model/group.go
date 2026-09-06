package model

// Group 分组（02 §3.4）：渠道集合 + 计费倍率 + 模型范围的逻辑单元。
// 索引：name 唯一部分索引（迁移 SQL）；disabled 的组渠道不参与调度。
type Group struct {
	ID   int64  `gorm:"primaryKey;column:id"`
	Name string `gorm:"column:name;type:varchar(100);not null"`

	// RateMultiplier 分组计费倍率，默认 1.0（numeric(10,4) 覆盖 Decimal 默认精度）
	RateMultiplier Decimal `gorm:"column:rate_multiplier;type:numeric(10,4);not null;default:1"`
	// AllowedModels 模型白名单 jsonb（空/空数组 = 不限）
	AllowedModels []string `gorm:"column:allowed_models;type:jsonb;serializer:json"`

	// RPM / 并发 分组级限制；0 = 不限制
	RPMLimit         int `gorm:"column:rpm_limit;not null;default:0"`
	ConcurrencyLimit int `gorm:"column:concurrency_limit;not null;default:0"`
	// DailyQuotaUSD 分组日额度（USD），可空
	DailyQuotaUSD *Decimal `gorm:"column:daily_quota_usd;type:numeric(20,10)"`

	// status: active | disabled
	Status string `gorm:"column:status;type:varchar(20);not null;default:'active'"`

	// FallbackModels 分组级降级链（模型名数组）。优先级：请求级 > 分组级 > 模型级（05 §9）
	FallbackModels []string `gorm:"column:fallback_models;type:jsonb;serializer:json"`
	SortOrder      int      `gorm:"column:sort_order;not null;default:0"`

	Timestamps
	SoftDelete
	Source
}

func (Group) TableName() string { return "groups" }
