// Package task 承载异步层的任务契约与 RabbitMQ 实现（01 §5.3/§7.1）：
//
//	业务代码只依赖 TaskEnqueuer 接口（内存实现可单测），不直接 import amqp091-go；
//	worker 侧用 Handler 注册表消费 task.critical/default/low 三队列。
package task

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// TaskType 决定由哪个 handler 处理；路由键 = 任务类型字符串（01 §7.1）。
type TaskType string

// QueueName 逻辑队列（映射到 broker 的 task.critical/default/low）。
type QueueName string

const (
	QueueCritical QueueName = "critical"
	QueueDefault  QueueName = "default"
	QueueLow      QueueName = "low"
)

// Task 契约（01 §5.3 权威定义，勿改字段语义）。
type Task struct {
	Type     TaskType        `json:"type"`                // 决定由哪个 handler 处理
	Queue    QueueName       `json:"queue"`               // Critical | Default | Low
	Payload  json.RawMessage `json:"payload"`             // 可序列化负载
	Key      string          `json:"key,omitempty"`       // 幂等键（如 settle:<request_id>），空表示不去重
	MaxRetry int             `json:"max_retry,omitempty"` // >0 覆盖队列默认 max_retry（10 §3.2）；0 = 队列默认
	Timeout  time.Duration   `json:"timeout"`             // 任务处理 ctx 超时
}

// Marshal 序列化为 AMQP body（应用层可额外在 header 放 x-task-key/x-retry-count）。
func (t Task) Marshal() ([]byte, error) {
	return json.Marshal(t)
}

// UnmarshalTask 反序列化 AMQP body。
func UnmarshalTask(b []byte) (Task, error) {
	var t Task
	if err := json.Unmarshal(b, &t); err != nil {
		return Task{}, fmt.Errorf("task: 反序列化任务负载失败: %w", err)
	}
	if t.Type == "" || t.Queue == "" {
		return Task{}, fmt.Errorf("task: 负载缺少 type/queue")
	}
	return t, nil
}

// Validate 校验任务归属与负载完整性（Enqueue 前调用）。
func (t Task) Validate(reg *Registry) error {
	if t.Type == "" {
		return fmt.Errorf("task: type 不能为空")
	}
	if t.Queue == "" {
		return fmt.Errorf("task: queue 不能为空")
	}
	if reg != nil {
		if q, ok := reg.QueueOf(t.Type); ok && q != t.Queue {
			return fmt.Errorf("task: %q 应投递到 %s，实际 %s（拓扑绑定不存在，会 mandatory 报错）", t.Type, q, t.Queue)
		}
	}
	if t.Timeout <= 0 {
		return fmt.Errorf("task: %q timeout 必须为正", t.Type)
	}
	return nil
}

// ── Registry：任务类型 ↔ 队列 绑定（拓扑即代码，01 §7.1）──────────────

// Registry 维护 TaskType → QueueName 映射；拓扑声明与 Enqueue 校验共用，
// 保证"投递目标必已绑定"，避免 mandatory 无路由。
type Registry struct {
	mu   sync.RWMutex
	byTy map[TaskType]QueueName
	byQ  map[QueueName][]TaskType
}

func NewRegistry() *Registry {
	return &Registry{
		byTy: make(map[TaskType]QueueName),
		byQ:  map[QueueName][]TaskType{QueueCritical: {}, QueueDefault: {}, QueueLow: {}},
	}
}

// Bind 注册任务类型到队列（可重复调用，重复绑定幂等）。
func (r *Registry) Bind(queue QueueName, types ...TaskType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ty := range types {
		if prev, ok := r.byTy[ty]; ok && prev != queue {
			panic(fmt.Sprintf("task: 任务类型 %q 已绑定 %s，不可改绑 %s", ty, prev, queue))
		}
		r.byTy[ty] = queue
	}
	for _, ty := range types {
		found := false
		for _, e := range r.byQ[queue] {
			if e == ty {
				found = true
				break
			}
		}
		if !found {
			r.byQ[queue] = append(r.byQ[queue], ty)
		}
	}
}

func (r *Registry) QueueOf(ty TaskType) (QueueName, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	q, ok := r.byTy[ty]
	return q, ok
}

func (r *Registry) Types(queue QueueName) []TaskType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]TaskType(nil), r.byQ[queue]...)
}

// DefaultRegistry 种子绑定（MVP：01 §7.2/§11 §3.2 只跑 usage:write/billing:settle/billing:refund；
// 其余任务类型在实现时经 Bind 补充——绑定与实现同 PR，杜绝"投递了但没 handler"）。
var DefaultRegistry = NewRegistry()

func init() {
	DefaultRegistry.Bind(QueueDefault, TaskUsageWrite)
	DefaultRegistry.Bind(QueueCritical, TaskBillingSettle)
	DefaultRegistry.Bind(QueueCritical, TaskBillingRefund)
	DefaultRegistry.Bind(QueueDefault, TaskPaymentQuery)
	DefaultRegistry.Bind(QueueDefault, TaskStatsAggregate)
	DefaultRegistry.Bind(QueueDefault, TaskBalanceNotify)
	DefaultRegistry.Bind(QueueLow, TaskDailyReconcile)
	DefaultRegistry.Bind(QueueLow, TaskChannelProbe)
	DefaultRegistry.Bind(QueueLow, TaskLogArchive)
	DefaultRegistry.Bind(QueueLow, TaskSubscriptionExpire)
	DefaultRegistry.Bind(QueueLow, TaskOrderClose)
	DefaultRegistry.Bind(QueueLow, TaskKeyExpire)
	DefaultRegistry.Bind(QueueLow, TaskQuotaReset)
	DefaultRegistry.Bind(QueueLow, TaskIdempotencyCleanup)
	DefaultRegistry.Bind(QueueCritical, TaskReserveReclaim)
}

// 任务类型常量（与 01 §7.2 任务清单一一对应；命名即路由键，禁改）。
const (
	TaskUsageWrite         TaskType = "usage:write"
	TaskBillingSettle      TaskType = "billing:settle"
	TaskBillingRefund      TaskType = "billing:refund"
	TaskPaymentQuery       TaskType = "payment:query"
	TaskStatsAggregate     TaskType = "stats:aggregate"
	TaskDailyReconcile     TaskType = "daily:reconcile"
	TaskChannelProbe       TaskType = "channel:probe"
	TaskLogArchive         TaskType = "log:archive"
	TaskSubscriptionExpire TaskType = "subscription:expire"
	TaskOrderClose         TaskType = "order:close"
	TaskKeyExpire          TaskType = "key:expire"
	TaskQuotaReset         TaskType = "quota:reset"
	TaskIdempotencyCleanup TaskType = "idempotency:cleanup"
	TaskBalanceNotify      TaskType = "balance:notify"
	TaskReserveReclaim     TaskType = "reserve:reclaim"
)
