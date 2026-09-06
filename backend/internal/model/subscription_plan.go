package model

// SubscriptionPlan 套餐定义（02 §7.2）。
type SubscriptionPlan struct {
	ID   int64  `gorm:"primaryKey;column:id"`
	Name string `gorm:"column:name;type:varchar(100);not null;uniqueIndex:uk_subscription_plans_name"`

	PriceUSD       Decimal  `gorm:"column:price_usd;type:numeric(20,10);not null;default:0"`
	DurationDays   int      `gorm:"column:duration_days;not null"`
	QuotaUSD       *Decimal `gorm:"column:quota_usd;type:numeric(20,10)"` // 套餐内含额度；空 = 不限量
	RateMultiplier Decimal  `gorm:"column:rate_multiplier;type:numeric(10,4);not null;default:1"`
	AllowedModels  []string `gorm:"column:allowed_models;type:jsonb;serializer:json"`

	// status: active | disabled
	Status string `gorm:"column:status;type:varchar(20);not null;default:'active'"`

	Timestamps
	SoftDelete
	Source
}

func (SubscriptionPlan) TableName() string { return "subscription_plans" }
