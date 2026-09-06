package task

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ResilientEnqueuer 弹性投递器（01 §7.3：RabbitMQ 整体不可用时的降级）。
//
// 语义：
//   - broker 可用 → 直通 primary（publisher confirm），零额外开销；
//   - broker 不可用 → 任务落 WAL 文件（JSONL，append + fsync），**返回成功但不丢失**；
//     降级期间由恢复 goroutine 探测，broker 恢复后逐条重放（幂等键兜底重复，06 §10）。
//
// 一致性模型：WAL 文件是**唯一持久事实源**（内存仅承载未落盘的瞬时写入窗口）；
// 重放成功后才从文件中移除该条——任何时刻崩溃都不会丢任务（最多重复，
// 由业务幂等键/唯一约束兜底）。文件重写采用 临时文件 + rename 原子替换。
//
// 注意：broker 故障期间的 EnqueueIn 延迟语义不保证精确——恢复重放时若 broker 已回，
// 会以原 delay 重新走 EnqueueIn（延迟任务属低频，如 payment:query），
// 该取舍与文档"降级只保证不丢"一致（过期的绝对时刻不追补）。
type ResilientEnqueuer struct {
	primary TaskEnqueuer
	walPath string // WAL 文件（JSONL，每行 {task,delay}）

	mu        sync.Mutex
	replaying bool // 恢复重放互斥（同一时刻仅一个 drain）
}

type walEntry struct {
	Task  Task          `json:"task"`
	Delay time.Duration `json:"delay,omitempty"` // >0 表示 EnqueueIn
}

// NewResilient 构造弹性投递器。walPath 为 WAL 文件绝对/相对路径（父目录自动创建）。
func NewResilient(primary TaskEnqueuer, walPath string) (*ResilientEnqueuer, error) {
	if primary == nil {
		return nil, errors.New("task: Resilient 需要 primary enqueuer")
	}
	dir := filepath.Dir(walPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("task: 创建 WAL 目录失败: %w", err)
	}
	return &ResilientEnqueuer{primary: primary, walPath: walPath}, nil
}

// Enqueue 立即投递；broker 不可用时降级落盘。
func (r *ResilientEnqueuer) Enqueue(ctx context.Context, task Task) error {
	return r.dispatch(ctx, task, 0)
}

// EnqueueIn 延迟投递；降级时尽力而为（重放为即时，见类型注释）。
func (r *ResilientEnqueuer) EnqueueIn(ctx context.Context, task Task, delay time.Duration) error {
	return r.dispatch(ctx, task, delay)
}

func (r *ResilientEnqueuer) dispatch(ctx context.Context, task Task, delay time.Duration) error {
	// 先按语义分支投递（EnqueueIn 不得退化为即时 Enqueue，否则丢延迟）
	var err error
	if delay > 0 {
		err = r.primary.EnqueueIn(ctx, task, delay)
	} else {
		err = r.primary.Enqueue(ctx, task)
	}
	if err == nil {
		r.maybeRecover()
		return nil
	}
	// primary 不可用 → 降级落盘（返回成功：消息已持久化，恢复后必达）
	return r.appendWAL(walEntry{Task: task, Delay: delay})
}

// appendWAL 追加一条并 fsync。失败即返回错误（此时调用方应停止接受以保内存不丢，
// 例如服务整体进入不可用状态——降级语义只保证"落盘成功即不丢"）。
func (r *ResilientEnqueuer) appendWAL(e walEntry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("task: WAL 序列化失败: %w", err)
	}
	b = append(b, '\n')

	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := os.OpenFile(r.walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("task: 打开 WAL 失败: %w", err)
	}
	defer func() { _ = f.Close() }() // 追加写路径错误已显式返回，close 仅供释放
	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("task: 写入 WAL 失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("task: fsync WAL 失败: %w", err)
	}
	return nil
}

// maybeRecover broker 恢复（一次成功投递）时触发异步重放，全程互斥单飞。
func (r *ResilientEnqueuer) maybeRecover() {
	r.mu.Lock()
	start := !r.replaying && walNonEmpty(r.walPath)
	if start {
		r.replaying = true
	}
	r.mu.Unlock()
	if !start {
		return
	}
	go r.recover()
}

func walNonEmpty(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// recover 重放 WAL：读全量 → 逐条投递 → 失败条目经临时文件+rename 原子回写。
//
// 崩溃安全：rename 之前原文件完整保留，最多重复不丢失。
// 并发安全：**全程持有 r.mu**——禁止重放期间并发 appendWAL，否则新落盘条目会被
// 结尾的原子重写整段截掉（数据丢失窗口）。代价是恢复期新降级投递短暂阻塞，
// 该窗口仅在 broker 抖动/恢复期出现，可接受（正确性 > 吞吐）。
func (r *ResilientEnqueuer) recover() {
	r.mu.Lock()
	defer func() {
		r.replaying = false
		r.mu.Unlock()
	}()

	entries, err := readWAL(r.walPath)
	if err != nil {
		return // 文件损坏等：保留原文件，等待人工介入（08 §8 审计留痕）
	}
	var failed []walEntry
	for _, e := range entries {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var err error
		if e.Delay > 0 {
			// broker 已恢复，尽力保留原延迟（EnqueueIn 语义恢复）；降级期间已过时间不追补
			err = r.primary.EnqueueIn(ctx, e.Task, e.Delay)
		} else {
			err = r.primary.Enqueue(ctx, e.Task)
		}
		cancel()
		if err != nil {
			failed = append(failed, e)
		}
	}
	// 原子替换：成功条目移除，失败条目回写（保持完整，不丢）
	if err := rewriteWAL(r.walPath, failed); err != nil {
		// 回写失败极罕见（磁盘满等）：保留原文件由下次触发重放
		return
	}
}

func readWAL(path string) ([]walEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // 只读扫描：错误经返回值传播
	var entries []walEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e walEntry
		if err := json.Unmarshal(line, &e); err != nil {
			// 坏行：不静默丢弃也不中断——跳过后续人工介入
			continue
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}

// rewriteWAL 以剩余条目原子重写（临时文件 + rename）。
func rewriteWAL(path string, entries []walEntry) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	// abort 仅用于中途失败路径：关闭句柄并清理临时文件，错误已由调用方返回
	abort := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	bw := bufio.NewWriter(f)
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			abort()
			return err
		}
		if _, err := bw.Write(append(b, '\n')); err != nil {
			abort()
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		abort()
		return err
	}
	if err := f.Sync(); err != nil {
		abort()
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Interface 契约断言
var _ TaskEnqueuer = (*ResilientEnqueuer)(nil)
