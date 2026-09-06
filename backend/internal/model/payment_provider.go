package model

// PaymentProvider 支付渠道定义（02 §7.3）。
// config 为 jsonb 加密存储密钥（信封加密，明文不入库）。
type PaymentProvider struct {
	ID   int64  `gorm:"primaryKey;column:id"`
	Code string `gorm:"column:code;type:varchar(30);not null;uniqueIndex:uk_payment_providers_code"` // alipay | wechat | stripe
	Name string `gorm:"column:name;type:varchar(100);not null"`

	Config    map[string]any `gorm:"column:config;type:jsonb;serializer:json"`
	Enabled   bool           `gorm:"column:enabled;not null;default:false"`
	SortOrder int            `gorm:"column:sort_order;not null;default:0"`

	Timestamps
	SoftDelete
	Source
}

func (PaymentProvider) TableName() string { return "payment_providers" }
