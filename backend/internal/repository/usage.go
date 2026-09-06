package repository

import (
	"context"

	"cloudfog/internal/model"
)

// ── usage_logs 调用日志（02 §6.1：只追加大表，禁止 UPDATE/DELETE）────────────────

// InsertUsageLog 追加一条调用日志。写入走任务链路（usage:write，03 §4.5 顺序 1），
// 幂等由 idempotency_records 保障（usage:<request_id>），本表无唯一键、只增不改。
func (r *Repository) InsertUsageLog(ctx context.Context, l *model.UsageLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}
