package model

import "time"

// Setting 系统配置（02 §8.3）：key 为 PK，value 存任意 JSON。
// 承载运行时可调项（注册开关、默认分组、风控阈值、通知模板、exchange_rates 等）。
type Setting struct {
	Key         string         `gorm:"primaryKey;column:key;type:varchar(64)"`
	Value       map[string]any `gorm:"column:value;type:jsonb;serializer:json"`
	Description string         `gorm:"column:description;type:varchar(255);not null;default:''"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;type:timestamptz;autoUpdateTime"`
	UpdatedBy   *int64         `gorm:"column:updated_by"`
}

func (Setting) TableName() string { return "settings" }
