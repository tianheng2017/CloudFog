package model

import "time"

// AuthSession 用户会话（07 §3.0）。token 明文仅登录时下发一次（sess_ 前缀），
// 库中只存 token_hash = SHA-256(salt || token)（与 API Key 同哈希约定 08 §3.1）。
// 吊销/过期：DELETE by hash（登出）；expires_at 由会话清理周期任务（01 §7.2）扫描。
type AuthSession struct {
	ID        int64  `gorm:"primaryKey;column:id"`
	TokenHash string `gorm:"column:token_hash;type:varchar(64);not null;uniqueIndex:uq_auth_sessions_token_hash"`
	UserID    int64  `gorm:"column:user_id;not null;index:idx_auth_sessions_user"`
	IP        string `gorm:"column:ip;type:varchar(45)"`
	UserAgent string `gorm:"column:user_agent;type:varchar(255)"`

	ExpiresAt    time.Time `gorm:"column:expires_at;not null"`
	LastActiveAt time.Time `gorm:"column:last_active_at;not null;default:now()"`
	CreatedAt    time.Time `gorm:"column:created_at;not null;default:now()"`
}

func (AuthSession) TableName() string { return "auth_sessions" }
