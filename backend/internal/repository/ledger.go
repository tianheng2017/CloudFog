package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"cloudfog/internal/model"
)

// ── billing_ledger 账单流水（02 §7.1：只追加，每笔余额变动留痕）────────────────

// InsertLedger 写一条流水。防重由调用方以部分唯一约束抢占（uq_ledger_settle/uq_ledger_refund，
// 06 §4.2 L3 / §10.3），重复插入会返回唯一键冲突（配合 IsUniqueViolation 判定）。
func (r *Repository) InsertLedger(ctx context.Context, l *model.BillingLedger) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// LedgerExistsByRequestID 是否已存在该 request_id 的流水（任意类型）。
// 用途：reserve:reclaim 死信补偿前检查是否已有 settle 流水（06 §3.3 L3 幂等），
// 以及客服/对账的快速判重。粒度判定（只查 settle 等）由调用方传 type 过滤演进。
func (r *Repository) LedgerExistsByRequestID(ctx context.Context, requestID string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.BillingLedger{}).
		Where("request_id = ? AND request_id <> ''", requestID).Count(&n).Error
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// IsUniqueViolation 判定错误是否 PostgreSQL 唯一键冲突（SQLSTATE 23505）。
// settle/refund/幂等表的最终防重（06 §10.3）都依赖此判定区分"重复到达"与"真实错误"。
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
