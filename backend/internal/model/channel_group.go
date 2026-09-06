package model

// ChannelGroup 渠道-分组关联（02 §4.3）：(channel_id, group_id) 联合主键，
// 决定某分组的用户可以使用哪些渠道。关联表：不软删。
type ChannelGroup struct {
	ChannelID int64 `gorm:"primaryKey;column:channel_id"`
	GroupID   int64 `gorm:"primaryKey;column:group_id"`
}

func (ChannelGroup) TableName() string { return "channel_groups" }
