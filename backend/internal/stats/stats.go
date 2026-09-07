// Package stats 计量聚合周期任务（02 §6.2 / 01 §7.2 stats:aggregate）。
// cron 触发 → 空负载（date 缺省 = 昨日[北京时间]，避免聚合未完成当日）→ handler 聚合落 usage_daily_stats。
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

// Engine stats:aggregate worker handler（worker 进程注册）。
type Engine struct {
	Repo *repository.Repository
}

// Register 注册 stats:aggregate handler。
func (e *Engine) Register() error {
	return task.RegisterHandler(task.TaskStatsAggregate, e.HandleAggregate)
}

type payload struct {
	// Date 聚合日（YYYY-MM-DD，北京时间日）；空 = 北京昨日。
	Date string `json:"date,omitempty"`
}

func (e *Engine) HandleAggregate(ctx context.Context, t task.Task) error {
	var p payload
	if len(t.Payload) > 0 {
		if err := json.Unmarshal(t.Payload, &p); err != nil {
			return fmt.Errorf("stats:aggregate 负载非法: %w", err)
		}
	}
	day := time.Now().AddDate(0, 0, -1) // 缺省北京昨日（当日数据可能仍不完整）
	if p.Date != "" {
		d, err := time.ParseInLocation("2006-01-02", p.Date, period.CST)
		if err != nil {
			return fmt.Errorf("stats:aggregate 日期非法: %w", err)
		}
		day = d
	}
	n, err := e.Repo.AggregateUsageDay(ctx, day)
	if err != nil {
		return err
	}
	slog.Default().Info("stats:aggregate 完成", "date", period.DayLabel(day), "rows", n)
	// 落表即完成；对账差异检测由 daily:reconcile 负责（B3-6）。
	return nil
}
