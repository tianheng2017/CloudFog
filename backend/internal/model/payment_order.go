package model

import "time"

// PaymentOrder 支付订单（02 §7.3）。幂等键体系：
//
//	order_no 唯一；provider_trade_no 唯一（部分，webhook 幂等）；refund_no 唯一（部分，退款幂等）。
//
// 部分唯一索引由迁移 SQL 建。生命周期无软删。
type PaymentOrder struct {
	ID      int64  `gorm:"primaryKey;column:id"`
	OrderNo string `gorm:"column:order_no;type:varchar(64);not null;uniqueIndex:uk_payment_orders_no"`
	UserID  int64  `gorm:"column:user_id;not null;index:idx_payment_orders_user_created,priority:1"`

	ProviderCode    string `gorm:"column:provider_code;type:varchar(30);not null"`
	ProviderTradeNo string `gorm:"column:provider_trade_no;type:varchar(128);not null;default:''"` // webhook 幂等键（空=未回调）

	Amount         Decimal `gorm:"column:amount;type:numeric(20,10);not null"` // 实付金额（支付渠道币种）
	Currency       string  `gorm:"column:currency;type:varchar(8);not null;default:'USD'"`
	ExchangeRate   Decimal `gorm:"column:exchange_rate;type:numeric(20,10);not null;default:1"`   // 当次汇率快照，下单时固化（§14.4）
	CreditedAmount Decimal `gorm:"column:credited_amount;type:numeric(20,10);not null;default:0"` // 实际入账额度（含赠送）

	// type: recharge | subscription
	Type   string `gorm:"column:type;type:varchar(20);not null"`
	PlanID *int64 `gorm:"column:plan_id"`

	// status: pending | paid | failed | closed | refunded | partial_refunded（06 §7.5 状态机）
	Status    string     `gorm:"column:status;type:varchar(20);not null;default:'pending';index:idx_payment_orders_status_expires,priority:1"`
	PaidAt    *time.Time `gorm:"column:paid_at;type:timestamptz"`
	ExpiredAt *time.Time `gorm:"column:expired_at;type:timestamptz;index:idx_payment_orders_status_expires,priority:2"`

	// RefundNo 平台退款单号（退款幂等键）；refunded_amount >= amount → refunded，否则 partial_refunded
	RefundNo       string         `gorm:"column:refund_no;type:varchar(64);not null;default:''"`
	RefundedAmount Decimal        `gorm:"column:refunded_amount;type:numeric(20,10);not null;default:0"`
	RefundedAt     *time.Time     `gorm:"column:refunded_at;type:timestamptz"`
	RawNotify      map[string]any `gorm:"column:raw_notify;type:jsonb;serializer:json"` // 原始回调，排障用

	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;autoCreateTime;index:idx_payment_orders_user_created,priority:2"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;autoUpdateTime"`
}

func (PaymentOrder) TableName() string { return "payment_orders" }
