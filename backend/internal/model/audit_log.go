package model

import "time"

// AuditLog 审计日志（02 §8.2）：只追加，不可变。before/after 仅记录非敏感字段，
// 凭证内容一律不记录；client_ip 掩码后存储（08 §8）。
// 索引：actor_id+created_at、target_type+target_id、action+created_at（迁移 SQL 建）。
type AuditLog struct {
	ID        int64  `gorm:"primaryKey;column:id"`
	ActorID   *int64 `gorm:"column:actor_id;index:idx_audit_actor_created,priority:1"` // 空 = system 动作
	ActorType string `gorm:"column:actor_type;type:varchar(20);not null"`              // user | admin | system
	// Action 点分命名动词（channel.credential.view），与权限点同命名空间（08 §5.2）
	Action     string `gorm:"column:action;type:varchar(64);not null;index:idx_audit_action_created,priority:1"`
	TargetType string `gorm:"column:target_type;type:varchar(30);not null;default:'';index:idx_audit_target,priority:1"`
	TargetID   string `gorm:"column:target_id;type:varchar(64);not null;default:'';index:idx_audit_target,priority:2"`

	Before map[string]any `gorm:"column:before;type:jsonb;serializer:json"` // 非敏感字段
	After  map[string]any `gorm:"column:after;type:jsonb;serializer:json"`  // success=变更后；failure=脱敏错误摘要

	// result: success | failure
	Result    string `gorm:"column:result;type:varchar(20);not null;default:'success'"`
	ClientIP  string `gorm:"column:client_ip;type:varchar(45);not null;default:''"` // 掩码后存储
	UserAgent string `gorm:"column:user_agent;type:varchar(512);not null;default:''"`

	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;autoCreateTime;index:idx_audit_actor_created,priority:2;index:idx_audit_action_created,priority:2"` // 不可变
}

func (AuditLog) TableName() string { return "audit_logs" }
