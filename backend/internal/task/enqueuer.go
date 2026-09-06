package task

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// TaskEnqueuer 业务代码唯一依赖的异步契约（01 §5.3）。
type TaskEnqueuer interface {
	Enqueue(ctx context.Context, task Task) error
	EnqueueIn(ctx context.Context, task Task, delay time.Duration) error
}

// RetryPolicy 重试策略（来自 10 §3.2 配置）。
type RetryPolicy struct {
	// RetryBuckets 应用级重试 TTL 桶（升序），消费失败按尝试次数选择档位。
	RetryBuckets []time.Duration
	// DelayBuckets EnqueueIn 档位（升序），取 ≥ delay 的最小档位。
	DelayBuckets []time.Duration
}

// pickBucket 取 ≥ want 的最小档位下标；无匹配返回 -1。
func pickBucket(buckets []time.Duration, want time.Duration) int {
	for i, b := range buckets {
		if b >= want {
			return i
		}
	}
	return -1
}

// ── RabbitMQ 实现 ────────────────────────────────────────────────

// RabbitEnqueuer 基于 amqp091-go 的发布器：durable 消息 + publisher confirm +
// mandatory（无路由立即报错，绝不静默丢，01 §7.1 反例警示）。
// 注意：AMQP Channel 非线程安全——Enqueue 内部串行化 + 失败自动重连重试一次。
type RabbitEnqueuer struct {
	url      string
	exchange string
	policy   RetryPolicy
	reg      *Registry

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
	// returnErr 记录 mandatory 无路由返回（Publish 不保证立即反映）。
	returnErr chan error
}

// NewRabbitEnqueuer 建立连接并开启 confirm 模式（连接建立即校验 URL 可达）。
func NewRabbitEnqueuer(ctx context.Context, url, exchange string, policy RetryPolicy, reg *Registry) (*RabbitEnqueuer, error) {
	if reg == nil {
		reg = DefaultRegistry
	}
	e := &RabbitEnqueuer{url: url, exchange: exchange, policy: policy, reg: reg, returnErr: make(chan error, 8)}
	if err := e.connect(ctx); err != nil {
		return nil, err
	}
	if err := e.ensureTopology(ctx); err != nil {
		e.Close()
		return nil, err
	}
	return e, nil
}

// dial 支持 ctx 的拨号（amqp091-go v1.12 的 Dial 无 ctx 版本）。
func dial(ctx context.Context, url string) (*amqp.Connection, error) {
	type result struct {
		conn *amqp.Connection
		err  error
	}
	done := make(chan result, 1)
	go func() {
		c, err := amqp.Dial(url)
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		return r.conn, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *RabbitEnqueuer) connect(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	// 关闭旧 channel，令旧 watchReturns goroutine 自然退出（channel 关闭即 range 结束）
	if e.ch != nil {
		_ = e.ch.Close()
	}
	conn, err := dial(ctx, e.url)
	if err != nil {
		return fmt.Errorf("task: 连接 RabbitMQ 失败: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("task: 打开 channel 失败: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("task: 开启 publisher confirm 失败: %w", err)
	}
	e.conn, e.ch = conn, ch
	go watchReturns(ch, e.returnErr)
	return nil
}

// watchReturns 监听 mandatory 无路由返回（Return）并把首个错误注入 returnErr。
func watchReturns(ch *amqp.Channel, out chan<- error) {
	for ret := range ch.NotifyReturn(make(chan amqp.Return, 4)) {
		select {
		case out <- fmt.Errorf("task: mandatory 无路由返回 reply_code=%d reply=%q rk=%q",
			ret.ReplyCode, ret.ReplyText, ret.RoutingKey):
		default:
		}
	}
}

func (e *RabbitEnqueuer) ensureTopology(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return EnsureTopology(e.ch, e.exchange, e.reg, e.policy.RetryBuckets, e.policy.DelayBuckets)
}

// Close 关闭连接。
func (e *RabbitEnqueuer) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var errs []error
	if e.ch != nil {
		errs = append(errs, e.ch.Close())
	}
	if e.conn != nil {
		errs = append(errs, e.conn.Close())
	}
	e.ch, e.conn = nil, nil
	return errors.Join(errs...)
}

// Enqueue 立即投递（durable + confirm + mandatory）。
func (e *RabbitEnqueuer) Enqueue(ctx context.Context, task Task) error {
	if err := task.Validate(e.reg); err != nil {
		return err
	}
	return e.publish(ctx, task, e.exchange, string(task.Type))
}

// EnqueueIn 延迟投递：经 delay.<档位> TTL 桶，取 ≥ delay 的最小档位；
// 请求超过最大档位直接报错（01 §7.1：当前仅 payment:query 使用）。
func (e *RabbitEnqueuer) EnqueueIn(ctx context.Context, task Task, delay time.Duration) error {
	if err := task.Validate(e.reg); err != nil {
		return err
	}
	idx := pickBucket(e.policy.DelayBuckets, delay)
	if idx < 0 {
		return fmt.Errorf("task: EnqueueIn(%s) 延迟 %s 超出最大档位 %s", task.Type, delay,
			e.policy.DelayBuckets[len(e.policy.DelayBuckets)-1])
	}
	ex := exchangeDelayPrefix + formatBucket(e.policy.DelayBuckets[idx])
	return e.publish(ctx, task, ex, string(task.Type))
}

// publish 统一发布：retry 计数不在此设（由 consumer 侧失败路径维护，见 handleDelivery）。
func (e *RabbitEnqueuer) publish(ctx context.Context, task Task, exchange, routingKey string) error {
	body, err := task.Marshal()
	if err != nil {
		return err
	}
	headers := amqp.Table{"x-task-type": string(task.Type)}
	if task.Key != "" {
		headers["x-task-key"] = task.Key
	}

	pub := amqp.Publishing{
		Headers:      headers,
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent, // durable：delivery mode 2
		Body:         body,
	}

	// AMQP channel 非线程安全（01 §7.1）：publish + confirm 等待必须持锁串行化，
	// 且与 connect/reconnect 互斥（避免 Wait 期间 channel 被替换/关闭）。
	e.mu.Lock()
	if e.ch == nil {
		e.mu.Unlock()
		return fmt.Errorf("task: publisher 未连接")
	}
	ch := e.ch
	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, pub)
	if err == nil && dc != nil && !dc.Wait() {
		err = errors.New("publish 未被 broker 确认")
	}
	e.mu.Unlock()

	if err == nil {
		select {
		case retErr := <-e.returnErr:
			return retErr
		default:
			return nil
		}
	}
	// 一次自动重连重试（滚动发布/网络抖动场景）
	if err2 := e.reconnectAndRetry(ctx, exchange, routingKey, pub); err2 != nil {
		return fmt.Errorf("task: publish %s@%s 失败且重连重试仍失败: %w（原错误: %v）", routingKey, exchange, err2, err)
	}
	return nil
}

func (e *RabbitEnqueuer) reconnectAndRetry(ctx context.Context, exchange, routingKey string, pub amqp.Publishing) error {
	if err := e.connect(ctx); err != nil {
		return err
	}
	if err := e.ensureTopology(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	if e.ch == nil {
		e.mu.Unlock()
		return fmt.Errorf("task: 重连后 channel 仍为空")
	}
	ch := e.ch
	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, pub)
	if err == nil && dc != nil && !dc.Wait() {
		err = errors.New("publish 未被 broker 确认")
	}
	e.mu.Unlock()
	return err
}

// ── 内存实现（单元测试用，01 §5.3）───────────────────────────────

// EnsureBrokerTopology 便捷函数：拨号 + 幂等声明拓扑后关闭。
// 供任意角色启动前调用（确保业务队列/桶/死信已就绪，不依赖是否启用消费者）。
func EnsureBrokerTopology(ctx context.Context, url, exchange string, policy RetryPolicy, reg *Registry) error {
	if reg == nil {
		reg = DefaultRegistry
	}
	if exchange == "" {
		exchange = DefaultExchange
	}
	conn, err := dial(ctx, url)
	if err != nil {
		return fmt.Errorf("task: 连接 RabbitMQ 失败: %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	return EnsureTopology(ch, exchange, reg, policy.RetryBuckets, policy.DelayBuckets)
}

// MemoryEnqueuer 进程内实现：Enqueue 投递到对应 handler（无 handler 丢弃），
// EnqueueIn 直接 Enqueue（测试语义：不等待延迟）。
type MemoryEnqueuer struct {
	mu       sync.Mutex
	handlers map[TaskType]func(context.Context, Task) error
	dropped  []Task
}

func NewMemoryEnqueuer(handlers map[TaskType]func(context.Context, Task) error) *MemoryEnqueuer {
	return &MemoryEnqueuer{handlers: handlers}
}

func (m *MemoryEnqueuer) Enqueue(ctx context.Context, task Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn := m.handlers[task.Type]; fn != nil {
		return fn(ctx, task)
	}
	m.dropped = append(m.dropped, task) // 无 handler：记录并"投递成功"（与 broker 无 binding 丢弃不同）
	return nil
}

func (m *MemoryEnqueuer) EnqueueIn(ctx context.Context, task Task, _ time.Duration) error {
	return m.Enqueue(ctx, task)
}

// verify interface compile-time
var _ TaskEnqueuer = (*RabbitEnqueuer)(nil)
var _ TaskEnqueuer = (*MemoryEnqueuer)(nil)
