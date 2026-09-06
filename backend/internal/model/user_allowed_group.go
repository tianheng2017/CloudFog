package model

// UserAllowedGroup 用户-分组允许关系（02 §3.4）：(user_id, group_id) 联合主键，
// 决定用户可切换到哪些分组。与 channel_groups 同为纯关联表：仅联合主键，不软删。
type UserAllowedGroup struct {
	UserID  int64 `gorm:"primaryKey;column:user_id"`
	GroupID int64 `gorm:"primaryKey;column:group_id"`
}

func (UserAllowedGroup) TableName() string { return "user_allowed_groups" }
