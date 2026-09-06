package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// flakyPrimary broker 故障模拟：前 fail 次调用失败，之后恢复。
type flakyPrimary struct {
	mu    sync.Mutex
	fail  int
	got   []Task // 成功投递记录（含延迟任务标记忽略）
	gotIn int
}

func (f *flakyPrimary) Enqueue(_ context.Context, t Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return errors.New("broker down")
	}
	f.got = append(f.got, t)
	return nil
}

func (f *flakyPrimary) EnqueueIn(_ context.Context, t Task, _ time.Duration) error {
	return f.Enqueue(context.Background(), t)
}

func (f *flakyPrimary) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

func TestResilient_DegradeThenReplay(t *testing.T) {
	dir := t.TempDir()
	wal := filepath.Join(dir, "tasks.wal")
	primary := &flakyPrimary{fail: 3}
	res, err := NewResilient(primary, wal)
	if err != nil {
		t.Fatalf("NewResilient: %v", err)
	}

	// broker 故障期：3 条降级落盘（返回成功）
	for i := 0; i < 3; i++ {
		tsk := Task{Type: TaskBillingSettle, Queue: QueueCritical, Timeout: time.Second}
		if err := res.Enqueue(context.Background(), tsk); err != nil {
			t.Fatalf("降级投递 #%d 应成功: %v", i, err)
		}
	}
	// 文件应有 3 条
	if st, _ := os.Stat(wal); st.Size() == 0 {
		t.Fatal("WAL 应非空")
	}

	// broker 恢复：第 4 条成功投递触发异步重放
	ok := Task{Type: TaskBillingSettle, Queue: QueueCritical, Timeout: time.Second}
	if err := res.Enqueue(context.Background(), ok); err != nil {
		t.Fatalf("恢复后投递失败: %v", err)
	}

	// 轮询：全部 4 条到达且 WAL 清空（原子重写）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if primary.len() == 4 {
			if st, err := os.Stat(wal); err == nil && st.Size() == 0 {
				return // PASS
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _ := os.Stat(wal)
	t.Fatalf("重放未收敛: got=%d wal_size=%d", primary.len(), st.Size())
}

// sawPrimary 可控 primary：fail 时返回 down；slow 时每次 Enqueue 睡 slow 时长（拉宽恢复窗口）。
type sawPrimary struct {
	mu   sync.Mutex
	fail bool
	slow bool
	got  []Task
}

func (s *sawPrimary) Enqueue(_ context.Context, t Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("down")
	}
	if s.slow {
		time.Sleep(200 * time.Millisecond)
	}
	s.got = append(s.got, t)
	return nil
}

func (s *sawPrimary) EnqueueIn(ctx context.Context, t Task, d time.Duration) error {
	return s.Enqueue(ctx, t)
}

func (s *sawPrimary) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

// 回归：重放进行中并发降级投递不得被恢复重写截掉（数据丢失窗口修复）。
func TestResilient_NoLossDuringConcurrentDegradeAndReplay(t *testing.T) {
	wal := filepath.Join(t.TempDir(), "tasks.wal")
	p := &sawPrimary{}
	res, err := NewResilient(p, wal)
	if err != nil {
		t.Fatal(err)
	}

	// 故障期落盘 A、B
	p.mu.Lock()
	p.fail = true
	p.mu.Unlock()
	for _, name := range []string{"A", "B"} {
		tsk := Task{Type: TaskBillingSettle, Queue: QueueCritical, Timeout: time.Second}
		if err := res.Enqueue(context.Background(), tsk); err != nil {
			t.Fatalf("%s 降级投递失败: %v", name, err)
		}
	}

	// broker 恢复但 slow：启动重放（处理 A/B 约 400ms），随后立刻抖动降级投递 C
	p.mu.Lock()
	p.fail, p.slow = false, true
	p.mu.Unlock()
	go res.recover()
	time.Sleep(50 * time.Millisecond) // 确保重放已开始
	p.mu.Lock()
	p.fail, p.slow = true, false
	p.mu.Unlock()
	tskC := Task{Type: TaskBillingSettle, Queue: QueueCritical, Timeout: time.Second}
	if err := res.Enqueue(context.Background(), tskC); err != nil {
		t.Fatalf("并发降级投递 C 失败: %v", err)
	}

	// 再恢复：D 成功投递触发第二轮 recover（处理 C）
	p.mu.Lock()
	p.fail, p.slow = false, false
	p.mu.Unlock()
	tskD := Task{Type: TaskBillingSettle, Queue: QueueCritical, Timeout: time.Second}
	if err := res.Enqueue(context.Background(), tskD); err != nil {
		t.Fatalf("D 投递失败: %v", err)
	}

	// A/B/C/D 全部应最终到达（C 不得被重写截掉），WAL 清空
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.len() == 4 {
			st, _ := os.Stat(wal)
			if st.Size() == 0 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _ := os.Stat(wal)
	t.Fatalf("存在丢失或未收敛: delivered=%d wal_size=%d（修复前 C 会被截掉，delivered=3）", p.len(), st.Size())
}

// 降级期间进程崩溃不丢：模拟先写 WAL，再"重启"（新实例读取既有 WAL 文件）。
func TestResilient_ReplayExistingWALAfterRestart(t *testing.T) {
	dir := t.TempDir()
	wal := filepath.Join(dir, "tasks.wal")

	// 第一次运行：故障期落盘 2 条
	primary1 := &flakyPrimary{fail: 99}
	res1, _ := NewResilient(primary1, wal)
	for i := 0; i < 2; i++ {
		tsk := Task{Type: TaskUsageWrite, Queue: QueueDefault, Timeout: time.Second}
		if err := res1.Enqueue(context.Background(), tsk); err != nil {
			t.Fatalf("降级投递失败: %v", err)
		}
	}

	// "重启"：新实例 + 健康 primary，next 成功投递触发既有 WAL 重放
	primary2 := &flakyPrimary{fail: 0}
	res2, _ := NewResilient(primary2, wal)
	tsk := Task{Type: TaskUsageWrite, Queue: QueueDefault, Timeout: time.Second}
	if err := res2.Enqueue(context.Background(), tsk); err != nil {
		t.Fatalf("重启后投递失败: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if primary2.len() == 3 {
			if st, _ := os.Stat(wal); st.Size() == 0 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _ := os.Stat(wal)
	t.Fatalf("重启重放未收敛: got=%d wal_size=%d", primary2.len(), st.Size())
}

func TestResilient_DelaySemanticsPreservedWhenUp(t *testing.T) {
	dir := t.TempDir()
	primary := &flakyPrimary{fail: 0}
	res, _ := NewResilient(primary, filepath.Join(dir, "t.wal"))
	tsk := Task{Type: TaskPaymentQuery, Queue: QueueDefault, Timeout: time.Second}
	if err := res.EnqueueIn(context.Background(), tsk, time.Minute); err != nil {
		t.Fatalf("EnqueueIn 失败: %v", err)
	}
	// broker 正常时不应降级落盘（延迟语义直接走 primary）
	if st, err := os.Stat(filepath.Join(dir, "t.wal")); err == nil && st.Size() > 0 {
		t.Fatal("broker 正常时不应写 WAL")
	}
}
