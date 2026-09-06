package model

import "time"

// Subscription 用户订阅实例（02 §7.2）。
// 发放最终防线：(id, period_index) 唯一（uq_subscription_period，迁移 SQL 建）。
// 续费时 period_index+1 与 expires_at 顺延在同事务完成；period 即幂等键 sub:{id}:{period_index}。
type Subscription struct {
	ID     int64 `gorm:"primaryKey;column:id"`
	UserID int64 `gorm:"column:user_id;not null;index:idx_subscriptions_user_status,priority:1"`
	PlanID int64 `gorm:"column:plan_id;not null"`

	// status: pending | active | expired | cancelled
	Status string `gorm:"column:status;type:varchar(20);not null;default:'pending';index:idx_subscriptions_user_status,priority:2;index:idx_subscriptions_status_expires,priority:1"`

	StartedAt   time.Time `gorm:"column:started_at;type:timestamptz;not null"`
	ExpiresAt   time.Time `gorm:"column:expires_at;type:timestamptz;not null;index:idx_subscriptions_status_expires,priority:2"` // 到期扫描
	PeriodIndex int       `gorm:"column:period_index;not null;default:1"`                                                        // 期次序号，首次为 1
	AutoRenew   bool      `gorm:"column:auto_renew;not null;default:false"`

	// QuotaTotal 为空表示不限量
	QuotaUsed  Decimal  `gorm:"column:quota_used;type:numeric(20,10);not null;default:0"`
	QuotaTotal *Decimal `gorm:"column:quota_total;type:numeric(20,10)"`

	Timestamps
}

func (Subscription) TableName() string { return "subscriptions" }
