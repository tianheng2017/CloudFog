// Scheduler 角色：进程内 cron（01 §7.3：单实例 + Redis 锁防护由上层保证）。
// 周期任务 = cron 到点 → periodicBuilder 构造负载 → Resilient(RabbitMQ,WAL) 投递。
// B1：业务 payload builder 未接线（builders 空），引擎就绪但无 job 触发，
// 因此 broker 不在也不影响 scheduler 进程存活（懒加载投递器，真正接线后生效）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"cloudfog/internal/config"
	"cloudfog/internal/task"
)

// periodicBuilder 构造某周期任务的负载（json.RawMessage）。
type periodicBuilder func() (json.RawMessage, error)

// periodicBuilders 周期任务 builder 注册表：业务模块接入时向此注入 builder（预留表驱动，B1 未接线）。
// builder 不存在则到点只记 debug 不投递（防无意义消息堆积）；读写一律经 periodicMu。
var (
	periodicMu       sync.RWMutex
	periodicBuilders = map[task.TaskType]periodicBuilder{}
)

// runScheduler 启动 cron 引擎并阻塞至 ctx 取消。
func runScheduler(ctx context.Context, cfg *config.Config, log *slog.Logger, runErr chan<- error) {
	// cron 表达式取自 config.Scheduler（10 §3.2）；空串 = 不启用（MVP 裁剪）
	specs := map[task.TaskType]string{
		task.TaskStatsAggregate:     cfg.Scheduler.StatsAggregate,
		task.TaskDailyReconcile:     cfg.Scheduler.DailyReconcile,
		task.TaskChannelProbe:       cfg.Scheduler.ChannelProbe,
		task.TaskLogArchive:         cfg.Scheduler.LogArchive,
		task.TaskSubscriptionExpire: cfg.Scheduler.SubscriptionExpire,
		task.TaskOrderClose:         cfg.Scheduler.OrderClose,
		task.TaskIdempotencyCleanup: cfg.Scheduler.IdempotencyCleanup,
		task.TaskKeyExpire:          cfg.Scheduler.KeyExpire,
		task.TaskQuotaReset:         cfg.Scheduler.QuotaReset,
		task.TaskBalanceNotify:      cfg.Scheduler.BalanceNotify,
		task.TaskReserveReclaim:     cfg.Scheduler.ReserveReclaim,
	}

	// 周期任务空负载 builder（worker 端 handler 依类型执行）：
	//  - reserve:reclaim 冻结额度死信补偿（06 §3.3 L3/M12，MVP 起）
	//  - stats:aggregate 计量聚合落 usage_daily_stats（b3-6，按 config.Scheduler.StatsAggregate cron 触发）
	periodicMu.Lock()
	for _, ty := range []task.TaskType{task.TaskReserveReclaim, task.TaskStatsAggregate} {
		periodicBuilders[ty] = func() (json.RawMessage, error) {
			return json.RawMessage("{}"), nil
		}
	}
	periodicMu.Unlock()

	periodicMu.RLock()
	wired := len(periodicBuilders)
	periodicMu.RUnlock()

	c := cron.New(cron.WithParser(cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)))
	for ty, spec := range specs {
		if spec == "" {
			continue
		}
		if _, err := c.AddFunc(spec, func() {
			publishPeriodic(ctx, cfg, log, ty)
		}); err != nil {
			runErr <- fmt.Errorf("scheduler 注册周期任务 %s(%q) 失败: %w", ty, spec, err)
			return
		}
	}

	log.Info("scheduler 已启动", "wired_builders", wired, "jobs", len(c.Entries()))
	c.Start()
	defer c.Stop()
	<-ctx.Done()
}

// publishPeriodic cron 到点回调：builder 存在才投递（经懒加载 Resilient 投递器）。
func publishPeriodic(ctx context.Context, cfg *config.Config, log *slog.Logger, ty task.TaskType) {
	periodicMu.RLock()
	b, ok := periodicBuilders[ty]
	periodicMu.RUnlock()
	if !ok {
		log.Debug("周期任务未接线（无 builder），跳过", "type", ty)
		return
	}
	payload, err := b()
	if err != nil {
		log.Error("周期任务负载构造失败", "type", ty, "error", err)
		return
	}
	q, ok := task.DefaultRegistry.QueueOf(ty)
	if !ok {
		log.Error("周期任务未注册队列，跳过", "type", ty)
		return
	}
	enq, err := schedulerEnqueuer(ctx, cfg)
	if err != nil {
		log.Warn("周期任务投递失败（投递器不可用）", "type", ty, "error", err)
		return
	}
	t := task.Task{Type: ty, Queue: q, Payload: payload, Timeout: 30 * time.Second}
	if err := enq.Enqueue(ctx, t); err != nil {
		log.Error("周期任务投递失败", "type", ty, "error", err)
	}
}

// schedulerEnqueuer 懒加载 Resilient 投递器（避免 broker 未起时阻塞 scheduler 启动）。
// 双检锁 + **失败可重试**：首次连不上（broker 抖动/晚于 scheduler 启动）下次到点会再试，
// 不得用 sync.Once 记住错误（否则 broker 恢复后周期任务永远无法投递）。
var (
	enqMu sync.Mutex
	enq   task.TaskEnqueuer
)

func schedulerEnqueuer(ctx context.Context, cfg *config.Config) (task.TaskEnqueuer, error) {
	enqMu.Lock()
	defer enqMu.Unlock()
	if enq != nil {
		return enq, nil
	}
	policy := task.RetryPolicy{
		RetryBuckets: cfg.RabbitMQ.RetryBuckets,
		DelayBuckets: cfg.RabbitMQ.DelayBuckets,
	}
	primary, err := task.NewRabbitEnqueuer(ctx, cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange, policy, task.DefaultRegistry)
	if err != nil {
		return nil, err
	}
	wal := os.Getenv("CLOUDFOG_WAL_PATH")
	if wal == "" {
		wal = "data/wal/tasks.wal"
	}
	res, err := task.NewResilient(primary, wal)
	if err != nil {
		_ = primary.Close() // 构造失败：关闭已建连接，返回原始错误
		return nil, err
	}
	enq = res
	return enq, nil
}
