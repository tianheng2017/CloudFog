package model

import "time"

// ModelPrice 定价（02 §5.2：带生效时间，支持历史回溯）。调价常态下价格独立成表，
// 每次调用按 effective_from <= now 取当前价并快照进 usage_logs，账单永远可复现。
// 索引：(model_id, effective_from) 唯一（uk_model_prices_model_eff，迁移 SQL 建）。
// 价格属历史事实，不软删。
type ModelPrice struct {
	ID      int64 `gorm:"primaryKey;column:id"`
	ModelID int64 `gorm:"column:model_id;not null"`

	Currency                    string   `gorm:"column:currency;type:varchar(8);not null;default:'USD'"`
	InputPricePer1K             Decimal  `gorm:"column:input_price_per_1k;type:numeric(20,10);not null;default:0"`
	OutputPricePer1K            Decimal  `gorm:"column:output_price_per_1k;type:numeric(20,10);not null;default:0"`
	CacheReadPricePer1K         *Decimal `gorm:"column:cache_read_price_per_1k;type:numeric(20,10)"`
	CacheWritePricePer1K        *Decimal `gorm:"column:cache_write_price_per_1k;type:numeric(20,10)"`
	LongContextThreshold        *int     `gorm:"column:long_context_threshold"`
	LongContextInputPricePer1K  *Decimal `gorm:"column:long_context_input_price_per_1k;type:numeric(20,10)"`
	LongContextOutputPricePer1K *Decimal `gorm:"column:long_context_output_price_per_1k;type:numeric(20,10)"`
	PerRequestPrice             Decimal  `gorm:"column:per_request_price;type:numeric(20,10);not null;default:0"`

	EffectiveFrom time.Time  `gorm:"column:effective_from;type:timestamptz;not null"`
	EffectiveTo   *time.Time `gorm:"column:effective_to;type:timestamptz"`

	Timestamps
}

func (ModelPrice) TableName() string { return "model_prices" }
