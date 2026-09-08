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

// LedgerExistsByRequestIDs 批量判定 request_id 是否已有流水（reclaim 用；单条 SQL 替代 N 次查询）。
func (r *Repository) LedgerExistsByRequestIDs(ctx context.Context, requestIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(requestIDs))
	if len(requestIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		RequestID string `gorm:"column:request_id"`
	}
	err := r.db.WithContext(ctx).Model(&model.BillingLedger{}).
		Select("request_id").
		Where("request_id IN ? AND request_id <> ''", requestIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.RequestID] = true
	}
	return out, nil
}

// IsUniqueViolation 判定错误是否 PostgreSQL 唯一键冲突（SQLSTATE 23505）。
// settle/refund/幂等表的最终防重（06 §10.3）都依赖此判定区分"重复到达"与"真实错误"。
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsForeignKeyViolation 判定错误是否 PostgreSQL 外键违例（SQLSTATE 23503）。
// 管理写端点绑定关联实体时用于把 FK 违例（引用不存在）映射为 404/400 而非 500。
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
