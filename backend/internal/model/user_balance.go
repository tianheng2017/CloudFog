package model

import "time"

// UserBalance 余额（02 §3.2）：与 users 分表——余额是高频更新热点行，分离避免更新余额锁住用户行。
// 约束：CHECK (balance >= 0)、CHECK (frozen >= 0)（迁移 SQL 建）。
// 并发控制见 06-billing §5（乐观锁 version）。
type UserBalance struct {
	UserID         int64   `gorm:"primaryKey;column:user_id"`
	Balance        Decimal `gorm:"column:balance;not null;default:0"` // 可用余额，恒非负
	Frozen         Decimal `gorm:"column:frozen;not null;default:0"`  // 预扣冻结中的额度
	TotalRecharged Decimal `gorm:"column:total_recharged;not null;default:0"`
	TotalConsumed  Decimal `gorm:"column:total_consumed;not null;default:0"`
	// QuotaResetAt 下次配额（日/月额度）重置绝对时间点，UTC（02 §13.2）
	QuotaResetAt *time.Time `gorm:"column:quota_reset_at;type:timestamptz"`
	// Version 乐观锁版本号（06-billing §5）
	Version   int64     `gorm:"column:version;not null;default:0"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;autoUpdateTime"`
}

func (UserBalance) TableName() string { return "user_balances" }
