package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudfog/internal/model"
)

// ConfirmInput 支付确认入参（06 §7.3：状态机 CAS + 幂等入账）。
type ConfirmInput struct {
	OrderNo         string
	ProviderTradeNo string
	PaidAt          time.Time
	RawNotify       map[string]any
}

// ErrOrderState 订单状态不允许入账（非 pending 且未与流水号一致，疑重复/状态冲突）。
var ErrOrderState = errors.New("repository: 订单状态不允许入账")

// ConfirmPayment 单事务幂等入账：
//  1. CAS：UPDATE payment_orders SET status='paid' ... WHERE id=? AND status='pending' AND provider_trade_no=”
//     （provider_trade_no 为空防覆盖；撞唯一由 CAS + provider_trade_no 唯一索引双保险）
//  2. rows=0 → 读回状态：已 paid 且同流水号 = 重复回调 → (false,nil) 幂等成功；否则 ErrOrderState
//  3. 余额入账（Ensure + UPDATE）+ billing_ledger(recharge) 同事务
//
// applied=true 表示本次完成入账（可写 L2 缓存）；false = 重复/已完成。
func (r *Repository) ConfirmPayment(ctx context.Context, in ConfirmInput) (applied bool, err error) {
	if in.OrderNo == "" || in.ProviderTradeNo == "" {
		return false, errors.New("repository: 确认入参缺失 order_no/provider_trade_no")
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 读订单（行锁防并发重复回调交错）
		var o model.PaymentOrder
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_no = ?", in.OrderNo).First(&o).Error; e != nil {
			return e
		}
		now := time.Now().UTC()
		amount := o.Amount // 金额以库中订单为准（notify 层已校验相等；此处权威防篡改）
		res := tx.Model(&model.PaymentOrder{}).
			Where("id = ? AND status = 'pending' AND provider_trade_no = ''", o.ID).
			Updates(map[string]any{"status": "paid", "provider_trade_no": in.ProviderTradeNo,
				"paid_at": in.PaidAt, "credited_amount": amount, "raw_notify": in.RawNotify,
				"updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var cur model.PaymentOrder
			if e := tx.Where("id = ?", o.ID).First(&cur).Error; e != nil {
				return e
			}
			if cur.Status == "paid" && cur.ProviderTradeNo == in.ProviderTradeNo {
				return nil // 重复回调：已入账，幂等成功
			}
			return fmt.Errorf("%w: order_no=%s status=%s", ErrOrderState, in.OrderNo, cur.Status)
		}
		// 余额行懒建 + 入账
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&model.UserBalance{UserID: o.UserID}).Error; e != nil {
			return e
		}
		if e := tx.Exec(
			`UPDATE user_balances SET balance = balance + ?, total_recharged = total_recharged + ?,
			        version = version + 1, updated_at = now() WHERE user_id = ?`,
			amount, amount, o.UserID).Error; e != nil {
			return e
		}
		var bal model.UserBalance
		if e := tx.Where("user_id = ?", o.UserID).First(&bal).Error; e != nil {
			return e
		}
		ledger := &model.BillingLedger{
			UserID: o.UserID, Type: "recharge", Amount: amount,
			BalanceAfter:   bal.Balance,
			PaymentOrderID: &o.ID, Description: "充值订单 " + o.OrderNo,
		}
		if e := tx.Create(ledger).Error; e != nil {
			return e
		}
		applied = true
		return nil
	})
	return applied, err
}
