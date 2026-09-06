package model

import "time"

// BillingLedger 账单流水（02 §7.1）：每一笔余额变动留痕，user_balances.balance 是其物化结果。
// 防重最终防线：两条部分唯一索引（uq_ledger_settle/uq_ledger_refund，迁移 SQL 建）——本表非分区表，
// 唯一约束可全局生效。只追加，无 SoftDelete/updated_at。
type BillingLedger struct {
	ID     int64 `gorm:"primaryKey;column:id"`
	UserID int64 `gorm:"column:user_id;not null;index:idx_ledger_user_created,priority:1"`

	// type: settle | refund | recharge | subscription | adjust | promo
	// 注意：预扣（reserve）不落库，只存在于 Redis 冻结额度（06 §3.2）
	Type string `gorm:"column:type;type:varchar(30);not null;index:idx_ledger_type_created,priority:1"`
	// Amount 带符号：负数为扣减，正数为增加
	Amount Decimal `gorm:"column:amount;type:numeric(20,10);not null"`
	// Overdraft 欠费额（默认 0）：仅结算补扣余额不足时为正；此时 BalanceAfter 记 0（06 §4.3）
	Overdraft Decimal `gorm:"column:overdraft;type:numeric(20,10);not null;default:0"`
	// BalanceAfter 变动后余额快照，恒非负
	BalanceAfter Decimal `gorm:"column:balance_after;type:numeric(20,10);not null"`

	RequestID      string `gorm:"column:request_id;type:varchar(64);not null;default:''"`
	UsageLogID     *int64 `gorm:"column:usage_log_id"`
	SubscriptionID *int64 `gorm:"column:subscription_id"`
	PaymentOrderID *int64 `gorm:"column:payment_order_id"`
	Description    string `gorm:"column:description;type:varchar(500);not null;default:''"`
	OperatorID     *int64 `gorm:"column:operator_id"` // 管理员手动调整时记录

	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;autoCreateTime;index:idx_ledger_user_created,priority:2;index:idx_ledger_type_created,priority:2"`
}

func (BillingLedger) TableName() string { return "billing_ledger" }
