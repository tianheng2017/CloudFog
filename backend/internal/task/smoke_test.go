//go:build integration

// E2E 冒烟（需 RabbitMQ）：go test -tags integration -run TestRabbitSmoke -v ./internal/task/
// 前置：docker compose -f deploy/docker-compose.dev.yml up -d rabbitmq
// 验证：拓扑声明幂等、Enqueue 即时路由、EnqueueIn 经 delay 桶 TTL 到期后回原队列（rk 保留）。
package task

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRabbitSmoke(t *testing.T) {
	url := os.Getenv("CLOUDFOG_RABBITMQ_URL")
	if url == "" {
		t.Skip("缺少 CLOUDFOG_RABBITMQ_URL")
	}
	ctx := context.Background()
	policy := RetryPolicy{
		RetryBuckets: []time.Duration{time.Second},
		DelayBuckets: []time.Duration{time.Second},
	}
	enq, err := NewRabbitEnqueuer(ctx, url, DefaultExchange, policy, DefaultRegistry)
	if err != nil {
		t.Fatalf("NewRabbitEnqueuer: %v", err)
	}
	defer enq.Close()

	// 1) 即时投递 critical → 消费到同一类型
	crit := Task{Type: TaskBillingSettle, Queue: QueueCritical, Payload: []byte(`{"n":1}`), Timeout: 5 * time.Second}
	if err := enq.Enqueue(ctx, crit); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got := consumeOne(t, ctx, url, "task.critical", 3*time.Second)
	if got != string(TaskBillingSettle) {
		t.Fatalf("critical 消费类型 = %q, want %q", got, TaskBillingSettle)
	}

	// 2) EnqueueIn 经 delay.1s 桶 → TTL 到期回 default 队列（rk 原样保留的关键路径）
	pay := Task{Type: TaskPaymentQuery, Queue: QueueDefault, Payload: []byte(`{"order":"o-1"}`), Timeout: 5 * time.Second}
	if err := enq.EnqueueIn(ctx, pay, time.Second); err != nil {
		t.Fatalf("EnqueueIn: %v", err)
	}
	// 应"延迟到账"：立即消费应拿不到
	if early := tryConsume(t, ctx, url, "task.default", 300*time.Millisecond); early != "" {
		t.Fatalf("EnqueueIn 应延迟 1s，但立即收到 %q", early)
	}
	got = consumeOne(t, ctx, url, "task.default", 3*time.Second)
	if got != string(TaskPaymentQuery) {
		t.Fatalf("延迟消费类型 = %q, want %q", got, TaskPaymentQuery)
	}
}

// TestRabbitConsumerRetryDLQ 全链路：RunConsumers → handler 恒失败 → 1s 重试桶
// → 重投再失败（attempts≥MaxRetry）→ Nack(requeue=false) → DLX → task.dlq。
// 覆盖此前从未被 E2E 触达的 worker 消费/重发/死信路径。
func TestRabbitConsumerRetryDLQ(t *testing.T) {
	url := os.Getenv("CLOUDFOG_RABBITMQ_URL")
	if url == "" {
		t.Skip("缺少 CLOUDFOG_RABBITMQ_URL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 清场，避免旧消息干扰
	conn, err := dial(ctx, url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	for _, q := range []string{"task.critical", "task.dlq"} {
		_, _ = ch.QueuePurge(q, false)
	}
	ch.Close()
	conn.Close()

	policy := RetryPolicy{RetryBuckets: []time.Duration{time.Second}}
	opts := ConsumerOptions{
		URL:      url,
		Exchange: DefaultExchange,
		Policy:   policy,
		Registry: DefaultRegistry,
		Consumers: map[QueueName]ConsumerConfig{
			QueueCritical: {Enabled: true, Concurrency: 1, Prefetch: 10, MaxRetry: 2, Timeout: time.Second},
		},
		Handlers: map[TaskType]HandlerFunc{
			TaskBillingSettle: func(context.Context, Task) error { return errors.New("boom") },
		},
		ShutdownTimeout: 5 * time.Second,
	}
	go func() {
		_ = RunConsumers(ctx, opts)
	}()

	enq, err := NewRabbitEnqueuer(ctx, url, DefaultExchange, policy, DefaultRegistry)
	if err != nil {
		t.Fatalf("NewRabbitEnqueuer: %v", err)
	}
	defer enq.Close()
	crit := Task{Type: TaskBillingSettle, Queue: QueueCritical, Payload: []byte(`{}`), Timeout: time.Second}
	if err := enq.Enqueue(ctx, crit); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// 轮询 task.dlq（首次失败→1s 桶→二次失败→DLQ；预算 10s）
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := tryConsume(t, ctx, url, "task.dlq", 300*time.Millisecond); got != "" {
			if got != string(TaskBillingSettle) {
				t.Fatalf("dlq 消息类型 = %q, want %q", got, TaskBillingSettle)
			}
			cancel()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	cancel()
	t.Fatal("任务未在预期时间内进入 task.dlq（重试/死信链路故障）")
}

// consumeOne 消费一条消息并返回 x-task-type header（autoAck）。
func consumeOne(t *testing.T, ctx context.Context, url, queue string, wait time.Duration) string {
	t.Helper()
	conn, err := dial(ctx, url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	defer ch.Close()
	msgs, err := ch.Consume(queue, "", true, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume %s: %v", queue, err)
	}
	select {
	case d := <-msgs:
		return headerString(d, "x-task-type")
	case <-time.After(wait):
		t.Fatalf("等待 %s 消息超时", queue)
		return ""
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return ""
	}
}

// headerString 兼容 broker 回传的 string / []byte 头值。
func headerString(d amqp.Delivery, key string) string {
	switch v := d.Headers[key].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

// tryConsume 尝试短时消费；拿不到返回 ""。
func tryConsume(t *testing.T, ctx context.Context, url, queue string, wait time.Duration) string {
	t.Helper()
	conn, err := dial(ctx, url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	defer ch.Close()
	msgs, err := ch.Consume(queue, "", true, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume %s: %v", queue, err)
	}
	select {
	case d := <-msgs:
		return headerString(d, "x-task-type")
	case <-time.After(wait):
		return ""
	}
}
