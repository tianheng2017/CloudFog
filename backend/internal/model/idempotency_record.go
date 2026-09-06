package model

import "time"

// IdempotencyRecord 幂等记录（02 §8.1）：快速返回的缓存，不是防重最终防线
// （最终防线是各业务表唯一约束，06 §10）。写入时机必须在任务成功之后——任务失败前写入
// 会被重试挡在门外导致漏结算。过期由 idempotency:cleanup 周期任务清理（01 §7.2）。
type IdempotencyRecord struct {
	ID int64 `gorm:"primaryKey;column:id"`

	// scope: settle | payment | usage | subscribe
	Scope          string `gorm:"column:scope;type:varchar(50);not null"`
	IdempotencyKey string `gorm:"column:idempotency_key;type:varchar(128);not null"`

	// Result 首次执行结果，重复请求直接返回
	Result    map[string]any `gorm:"column:result;type:jsonb;serializer:json"`
	ExpiresAt time.Time      `gorm:"column:expires_at;type:timestamptz;not null;index:idx_idem_expires_at"`
	CreatedAt time.Time      `gorm:"column:created_at;type:timestamptz;autoCreateTime"`
}

// TableName 实现 table 名（约定：scope+key 唯一索引由迁移 SQL 建 uk_idem_scope_key）。
func (IdempotencyRecord) TableName() string { return "idempotency_records" }
