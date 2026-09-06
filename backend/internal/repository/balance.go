package repository

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudfog/internal/model"
)

// ── user_balances 资金原语（06 §5：并发安全 = 条件更新，不使用悲观锁）────────────────

// EnsureBalance 确保用户余额行存在（懒创建，0 余额）。重复调用幂等。
// 余额行只在首次产生资金动作（充值/结算）前按需创建，避免给每个注册用户预建热点行。
func (r *Repository) EnsureBalance(ctx context.Context, userID int64) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.UserBalance{UserID: userID}).Error
}

// BalanceByUserID 读取余额快照。无行（从未产生资金动作）返回 nil——调用方按 0 余额处理。
func (r *Repository) BalanceByUserID(ctx context.Context, userID int64) (*model.UserBalance, error) {
	var b model.UserBalance
	err := r.db.WithContext(ctx).First(&b, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// BalanceDelta 余额变动增量（结算/退款/调整共用同一原语）。
// 语义（06 §5.3）：Balance 为可用余额增量（扣减为负），Frozen 为冻结余额增量（释放为负），
// TotalConsumed 为累计消费增量（只增不减）。增量是否允许由 DB 条件决定，
// 本原语不承载任何业务决策（多退少补/欠费判定归结算领域，06 §4.3）。
type BalanceDelta struct {
	Balance       decimal.Decimal
	Frozen        decimal.Decimal
	TotalConsumed decimal.Decimal
}

// ApplyBalanceDelta 条件更新余额：把充足性检查交给数据库
// （UPDATE ... WHERE balance + delta >= 0 AND frozen + delta >= 0），与 user_balances 的
// CHECK (balance >= 0) / CHECK (frozen >= 0) 口径一致（06 §5.3 示例 SQL）。
// 返回 applied=false 表示余额/冻结不足（未发生任何变更），rows affected 恒 ≤ 1。
func (r *Repository) ApplyBalanceDelta(ctx context.Context, userID int64, d BalanceDelta) (bool, error) {
	res := r.db.WithContext(ctx).Exec(
		`UPDATE user_balances
		 SET balance = balance + ?, frozen = frozen + ?, total_consumed = total_consumed + ?,
		     version = version + 1, updated_at = now()
		 WHERE user_id = ? AND balance + ? >= 0 AND frozen + ? >= 0`,
		d.Balance, d.Frozen, d.TotalConsumed, userID, d.Balance, d.Frozen)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
