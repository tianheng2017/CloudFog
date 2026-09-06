package task

import (
	"context"
	"testing"
	"time"
)

// 无启用的消费者组时 RunConsumers 应立即返回（不无限空转阻塞）。
func TestRunConsumersNoneEnabled(t *testing.T) {
	opts := ConsumerOptions{
		Consumers: map[QueueName]ConsumerConfig{
			QueueCritical: {Enabled: false},
			QueueDefault:  {Enabled: false},
			QueueLow:      {Enabled: false},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RunConsumers(ctx, opts); err != nil {
		t.Fatalf("无启用组应返回 nil: %v", err)
	}
}

func TestFormatBucket(t *testing.T) {
	cases := map[time.Duration]string{
		500 * time.Millisecond: "500ms",
		5 * time.Second:        "5s",
		90 * time.Second:       "90s", // 非整分必须秒粒度，杜绝截断成 1m
		time.Minute:            "1m",
		5 * time.Minute:        "5m",
		15 * time.Minute:       "15m",
		30 * time.Minute:       "30m",
		time.Hour:              "1h",
		2 * time.Hour:          "2h",
	}
	for d, want := range cases {
		if got := formatBucket(d); got != want {
			t.Errorf("formatBucket(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestPickBucket(t *testing.T) {
	b := []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute}
	cases := []struct {
		want time.Duration
		idx  int
	}{
		{1 * time.Minute, 0},
		{5 * time.Minute, 0},
		{6 * time.Minute, 1},
		{15 * time.Minute, 1},
		{30 * time.Minute, 2},
	}
	for _, c := range cases {
		if got := pickBucket(b, c.want); got != c.idx {
			t.Errorf("pickBucket(%s) = %d, want %d", c.want, got, c.idx)
		}
	}
	if got := pickBucket(b, 31*time.Minute); got != -1 {
		t.Errorf("超档位应返回 -1, got %d", got)
	}
}

func TestRetryBucket(t *testing.T) {
	b := []time.Duration{5 * time.Second, time.Minute, 5 * time.Minute}
	if got := retryBucket(b, 0); got != 0 { // 首次失败 → 5s
		t.Fatalf("attempt=0 bucket = %d, want 0", got)
	}
	if got := retryBucket(b, 2); got != 2 { // 第 3 次失败 → 5m
		t.Fatalf("attempt=2 bucket = %d, want 2", got)
	}
	if got := retryBucket(b, 99); got != 2 { // 封顶末档
		t.Fatalf("attempt=99 bucket = %d, want 2", got)
	}
	if got := retryBucket(nil, 0); got != -1 {
		t.Fatalf("空桶应返回 -1, got %d", got)
	}
}

func TestTaskMarshalRoundTrip(t *testing.T) {
	orig := Task{
		Type:     TaskBillingSettle,
		Queue:    QueueCritical,
		Payload:  []byte(`{"request_id":"r-1"}`),
		Key:      "settle:r-1",
		MaxRetry: 10,
		Timeout:  30 * time.Second,
	}
	b, err := orig.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalTask(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != orig.Type || got.Queue != orig.Queue || got.Key != orig.Key || got.MaxRetry != orig.MaxRetry {
		t.Fatalf("round-trip 不一致: %+v", got)
	}
}

func TestTaskMarshalRejectsEmpty(t *testing.T) {
	if _, err := UnmarshalTask([]byte(`{}`)); err == nil {
		t.Fatal("空负载应报错")
	}
}

func TestRegistryBindAndValidate(t *testing.T) {
	r := NewRegistry()
	r.Bind(QueueCritical, TaskBillingSettle)
	if q, ok := r.QueueOf(TaskBillingSettle); !ok || q != QueueCritical {
		t.Fatalf("QueueOf = %q,%v", q, ok)
	}
	// Validate：跨队列投递报错
	task := Task{Type: TaskBillingSettle, Queue: QueueDefault, Timeout: time.Second}
	if err := task.Validate(r); err == nil {
		t.Fatal("跨队列投递应报错")
	}
	// 合法投递
	task.Queue = QueueCritical
	if err := task.Validate(r); err != nil {
		t.Fatalf("合法投递应通过: %v", err)
	}
}
