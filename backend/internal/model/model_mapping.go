package model

// ModelMapping 模型映射（02 §5.3）。channel_id 空 = 全局映射。
// 解析顺序：渠道级精确 → 渠道级通配 → 全局精确 → 全局通配 → 原样透传。
// 索引：(channel_id, alias)、(alias) —— 迁移 SQL 建。映射为运维配置，不软删。
type ModelMapping struct {
	ID            int64  `gorm:"primaryKey;column:id"`
	ChannelID     *int64 `gorm:"column:channel_id"`                       // 空 = 全局映射
	Alias         string `gorm:"column:alias;type:varchar(100);not null"` // 支持通配，如 gpt-4*
	UpstreamModel string `gorm:"column:upstream_model;type:varchar(100);not null"`
	Priority      int    `gorm:"column:priority;not null;default:0"`

	Timestamps
}

func (ModelMapping) TableName() string { return "model_mappings" }
