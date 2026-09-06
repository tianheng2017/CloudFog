package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudfog/internal/model"
)

// ── idempotency_records（02 §8.1 / 06 §10.2 L2）────────────────
//
// 写入时机纪律：仅在业务动作**成功后**写（06 §4.2：处理前抢占会挡重试导致漏结算）。
// 本层是快速返回缓存，不是防重最终防线（最终防线 = 各业务表唯一约束）。

// GetIdempotency 查 L2 幂等结果。未命中或已过期一律返回 (nil, nil)（过期记录等价于未命中，
// 由调用方重新执行，靠业务表唯一约束防重；物理清理归 idempotency:cleanup 周期任务）。
func (r *Repository) GetIdempotency(ctx context.Context, scope, key string) (*model.IdempotencyRecord, error) {
	var rec model.IdempotencyRecord
	err := r.db.WithContext(ctx).
		Where("scope = ? AND idempotency_key = ? AND expires_at > now()", scope, key).
		First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// PutIdempotency 写入/刷新 L2 幂等记录（成功结果缓存）。并发重复投递同一键时
// 用唯一约束（uk_idem_scope_key）幂等覆盖，不报错。
func (r *Repository) PutIdempotency(ctx context.Context, scope, key string, result map[string]any, ttl time.Duration) error {
	rec := &model.IdempotencyRecord{
		Scope:          scope,
		IdempotencyKey: key,
		Result:         result,
		ExpiresAt:      time.Now().UTC().Add(ttl),
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "scope"}, {Name: "idempotency_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"result", "expires_at"}),
		}).
		Create(rec).Error
}
