package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloudfog/internal/task"
)

// Producer 计量/结算投递器：把网关终态转成 usage:write + billing:settle 任务（03 §4.5）。
type Producer struct {
	Enq task.TaskEnqueuer
}

const defaultTaskTimeout = 30 * time.Second

func (p *Producer) build(ty task.TaskType, q task.QueueName, key string, payload any) (task.Task, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return task.Task{}, err
	}
	return task.Task{Type: ty, Queue: q, Key: key, Payload: raw, Timeout: defaultTaskTimeout}, nil
}

// SendUsage 投递 usage:write（default 队列，幂等键 usage:<request_id>）。
func (p *Producer) SendUsage(ctx context.Context, u *UsageLogPayload) error {
	t, err := p.build(task.TaskUsageWrite, task.QueueDefault, "usage:"+u.RequestID, u)
	if err != nil {
		return err
	}
	if err := p.Enq.Enqueue(ctx, t); err != nil {
		return fmt.Errorf("usage:write 投递失败: %w", err)
	}
	return nil
}

// SendSettle 投递 billing:settle（critical 队列，幂等键 settle:<request_id>）。
func (p *Producer) SendSettle(ctx context.Context, s *SettlePayload) error {
	t, err := p.build(task.TaskBillingSettle, task.QueueCritical, "settle:"+s.RequestID, s)
	if err != nil {
		return err
	}
	if err := p.Enq.Enqueue(ctx, t); err != nil {
		return fmt.Errorf("billing:settle 投递失败: %w", err)
	}
	return nil
}
