package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"cloudfog/internal/pkg/period"
	"cloudfog/internal/repository"
	"cloudfog/internal/task"
)

// reconcileEpsilon 对账允许误差（decimal 6 位内视为一致；统计为聚合整数值，几乎为 0 差异）。
const reconcileEpsilon = 0.000001

// ReconcileEngine daily:reconcile worker handler：比对北京昨日 usage↔settle，差异记日志/告警。
type ReconcileEngine struct {
	Repo *repository.Repository
}

func (e *ReconcileEngine) Register() error {
	return task.RegisterHandler(task.TaskDailyReconcile, e.HandleReconcile)
}

func (e *ReconcileEngine) HandleReconcile(ctx context.Context, t task.Task) error {
	var p payload // 复用 {date} 负载
	if len(t.Payload) > 0 {
		if err := json.Unmarshal(t.Payload, &p); err != nil {
			return fmt.Errorf("daily:reconcile 负载非法: %w", err)
		}
	}
	day := time.Now().AddDate(0, 0, -1) // 缺省北京昨日
	if p.Date != "" {
		d, err := time.ParseInLocation("2006-01-02", p.Date, period.CST)
		if err != nil {
			return fmt.Errorf("daily:reconcile 日期非法: %w", err)
		}
		day = d
	}
	res, err := e.Repo.ReconcileDay(ctx, day)
	if err != nil {
		return err
	}
	diff := res.Diff.InexactFloat64()
	if diff < 0 {
		diff = -diff
	}
	if diff > reconcileEpsilon {
		// 差异超出容忍：usage 应计 ↔ settle 已扣不一致 → 告警级日志（06 §7 差异人工排查）
		slog.Error("daily:reconcile 差异超限", "date", period.DayLabel(day),
			"usage_total", res.UsageTotal.String(), "settled_total", res.SettledTotal.String(),
			"diff", res.Diff.String(), "usage_count", res.UsageCount, "settle_count", res.SettleCount)
	} else {
		slog.Info("daily:reconcile 一致", "date", period.DayLabel(day),
			"usage_total", res.UsageTotal.String(), "settled_total", res.SettledTotal.String(),
			"usage_count", res.UsageCount, "settle_count", res.SettleCount)
	}
	return nil
}
