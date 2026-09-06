package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ConsumerConfig 单队列消费者组配置（与 10 §3.2 对应，MVP low 不启用消费）。
type ConsumerConfig struct {
	Enabled     bool
	Concurrency int
	Prefetch    int
	MaxRetry    int
	Timeout     time.Duration
}

// HandlerFunc 任务处理函数；返回 nil 即成功（手动 ack）。
type HandlerFunc func(ctx context.Context, task Task) error

// ConsumerOptions 组装 worker 角色。
type ConsumerOptions struct {
	URL      string
	Exchange string
	Policy   RetryPolicy
	Registry *Registry

	// Consumers 按逻辑队列配置；Disabled 的队列不启动消费者组（任务仍可投递）。
	Consumers map[QueueName]ConsumerConfig
	// Handlers 任务处理注册表：缺失 handler 的消息 Nack 进 DLQ（P0 告警），不静默吞。
	Handlers map[TaskType]HandlerFunc
	// ShutdownTimeout 优雅关闭上限（等待在途处理完成后 cancel）。
	ShutdownTimeout time.Duration
}

// RunConsumers 启动全部启用的消费者组并阻塞至 ctx 取消。
// 每并发工作单元独占一个 AMQP channel（01 §7.1：channel 非线程安全，禁多 goroutine 共享）。
// 启动前自包含地 EnsureTopology：broker 卷清空/重建后也能工作，不依赖外部预热。
func RunConsumers(ctx context.Context, opts ConsumerOptions) error {
	reg := opts.Registry
	if reg == nil {
		reg = DefaultRegistry
	}
	if opts.Exchange == "" {
		opts.Exchange = DefaultExchange
	}

	var enabled int
	for _, cfg := range opts.Consumers {
		if cfg.Enabled && cfg.Concurrency > 0 {
			enabled++
		}
	}
	if enabled == 0 {
		return nil // 无启用的消费者组：worker 角色本阶段不消费（11 §3.2），不阻塞
	}

	conn, err := dial(ctx, opts.URL)
	if err != nil {
		return fmt.Errorf("task: worker 连接 RabbitMQ 失败: %w", err)
	}
	defer conn.Close()

	// 启动前拓扑自愈（幂等声明，失败即整体失败——避免所有 worker 因队列缺失静默退出）
	if err := ensureTopologyOnce(conn, opts.Exchange, reg, opts.Policy); err != nil {
		return fmt.Errorf("task: worker 启动前拓扑声明失败: %w", err)
	}

	var wg sync.WaitGroup
	for q, cfg := range opts.Consumers {
		if !cfg.Enabled || cfg.Concurrency <= 0 {
			continue
		}
		if cfg.MaxRetry <= 0 {
			return fmt.Errorf("task: %s 消费者组 max_retry 必须为正", q)
		}
		if cfg.Prefetch <= 0 {
			return fmt.Errorf("task: %s 消费者组 prefetch 必须为正（≤0 会退化为无限预取）", q)
		}
		queue, err := BusinessQueue(q)
		if err != nil {
			return err
		}
		for i := 0; i < cfg.Concurrency; i++ {
			wg.Add(1)
			go func(queue string, cfg ConsumerConfig) {
				defer wg.Done()
				worker(ctx, conn, queue, cfg, opts)
			}(queue, cfg)
		}
	}
	<-ctx.Done()
	// 优雅关闭：等待在途任务完成（每 worker 同步处理单条消息后自然让出）
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	timeout := opts.ShutdownTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	select {
	case <-done:
	case <-time.After(timeout):
		return fmt.Errorf("task: 优雅关闭超时（%s）", timeout)
	}
	return nil
}

// ensureTopologyOnce 用临时 channel 幂等声明拓扑后关闭。
func ensureTopologyOnce(conn *amqp.Connection, exchange string, reg *Registry, p RetryPolicy) error {
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("打开拓扑声明 channel 失败: %w", err)
	}
	defer ch.Close()
	return EnsureTopology(ch, exchange, reg, p.RetryBuckets, p.DelayBuckets)
}

// worker 单并发消费者：独占 channel，同步处理单条消息（处理完才取下一条），
// 失败按重试策略路由，成功/失败终局均手动 ack/nack。
func worker(ctx context.Context, conn *amqp.Connection, queue string, cfg ConsumerConfig, opts ConsumerOptions) {
	ch, err := conn.Channel()
	if err != nil {
		slog.Error("task: worker 打开 channel 失败", "queue", queue, "error", err)
		return
	}
	defer ch.Close()
	// 消费者 channel 也需开启 confirm：重试桶重发必须真实验证 broker 确认后再 Ack 原消息
	// （01 §7.3：先确认重发成功，再 ack），否则确认形同虚设。
	if err := ch.Confirm(false); err != nil {
		slog.Error("task: 消费者 channel 开启 confirm 失败", "queue", queue, "error", err)
		return
	}
	if err := ch.Qos(cfg.Prefetch, 0, false); err != nil {
		slog.Error("task: 设置 QoS 失败", "queue", queue, "error", err)
		return
	}
	msgs, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		slog.Error("task: 消费失败", "queue", queue, "error", err)
		return
	}
	slog.Info("task: 消费者组已启动", "queue", queue, "prefetch", cfg.Prefetch)

	for {
		select {
		case <-ctx.Done():
			slog.Info("task: 消费者组退出", "queue", queue)
			return
		case d, ok := <-msgs:
			if !ok {
				// 连接/通道异常关闭：worker 退出并留痕（重连由 supervisor 层负责，阶段 3）
				slog.Error("task: 消费通道关闭，worker 退出（待 supervisor 重连）", "queue", queue)
				return
			}
			// 在途任务使用独立超时 ctx：优雅关闭只停止取新消息，
			// 不取消正在处理的任务（01 §7.3：等其完成并 ack，未 ack 才由 broker 重新入队）。
			handleDelivery(ctx, ch, d, cfg, opts)
		}
	}
}

func handleDelivery(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, cfg ConsumerConfig, opts ConsumerOptions) {
	taskMsg, err := UnmarshalTask(d.Body)
	if err != nil {
		// 负载不可解析 = 结构性错误：进 DLQ 由运维排查，不重试
		slog.Error("task: 任务负载解析失败，转 DLQ", "error", err)
		_ = ch.Nack(d.DeliveryTag, false, false)
		return
	}
	fn := opts.Handlers[taskMsg.Type]
	if fn == nil {
		slog.Error("task: 无 handler，转 DLQ", "type", taskMsg.Type)
		_ = ch.Nack(d.DeliveryTag, false, false)
		return
	}

	timeout := taskMsg.Timeout
	if timeout <= 0 {
		timeout = cfg.Timeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second // 兜底：WithTimeout(0) 会立即取消
	}
	// 基于 context.Background() 派生：优雅关闭（父 ctx 取消）只停止取新消息，
	// 不取消在途任务——在途任务用满自身超时完成并 Ack（01 §7.3）。
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	err = fn(runCtx, taskMsg)
	cancel()
	if err == nil {
		_ = ch.Ack(d.DeliveryTag, false)
		return
	}

	attempts := retryCount(d)
	if int(attempts) >= cfg.MaxRetry {
		// 重试耗尽：Nack(requeue=false) → DLX → task.dlq（P0 告警）
		slog.Error("task: 重试耗尽进 DLQ", "type", taskMsg.Type, "attempts", attempts, "error", err)
		_ = ch.Nack(d.DeliveryTag, false, false)
		return
	}
	// 应用级重试：经 fanout 桶（rk 仍为任务类型，TTL 到期回原队列）
	bucket := retryBucket(opts.Policy.RetryBuckets, int(attempts))
	if bucket < 0 {
		_ = ch.Nack(d.DeliveryTag, false, false)
		return
	}
	ex := exchangeRetryPrefix + formatBucket(opts.Policy.RetryBuckets[bucket])
	body := d.Body
	headers := amqp.Table{"x-task-type": string(taskMsg.Type), "x-retry-count": attempts + 1}
	pub := amqp.Publishing{
		Headers:      headers,
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}
	dc, perr := ch.PublishWithDeferredConfirmWithContext(ctx, ex, string(taskMsg.Type), true, false, pub)
	if perr == nil && dc != nil && !dc.Wait() {
		perr = errors.New("重试桶投递未被 broker 确认")
	}
	if perr != nil {
		// 重发失败：原消息 requeue=true，broker 重新入队（幂等键兜底重复消费）
		slog.Warn("task: 重试桶投递失败，原消息 requeue", "type", taskMsg.Type, "error", perr)
		_ = ch.Nack(d.DeliveryTag, false, true)
		return
	}
	slog.Info("task: 已投入重试桶", "type", taskMsg.Type, "attempt", attempts+1, "bucket", formatBucket(opts.Policy.RetryBuckets[bucket]))
	// 重试桶投递成功后再 Ack 原消息（01 §7.3：先确认重发成功，再 ack）
	_ = ch.Ack(d.DeliveryTag, false)
}

// retryCount 读取 header x-retry-count（应用级已重试次数）。
func retryCount(d amqp.Delivery) int32 {
	if d.Headers == nil {
		return 0
	}
	switch v := d.Headers["x-retry-count"].(type) {
	case int32:
		return v
	case int64:
		return int32(v)
	case int:
		return int32(v)
	default:
		return 0
	}
}

// retryBucket 按已失败次数选桶：0→第 0 档、1→第 1 档…封顶末档（近似指数退避）。
func retryBucket(buckets []time.Duration, attempts int) int {
	if len(buckets) == 0 {
		return -1
	}
	if attempts >= len(buckets) {
		return len(buckets) - 1
	}
	return attempts
}
