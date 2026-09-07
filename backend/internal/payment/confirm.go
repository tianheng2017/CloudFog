package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloudfog/internal/repository"
	"cloudfog/internal/task"
)

// ConfirmPayload payment:confirm 负载（自包含）。Amount 以 DB 订单为权威，payload 仅审计参考。
type ConfirmPayload struct {
	OrderNo         string         `json:"order_no"`
	ProviderTradeNo string         `json:"provider_trade_no"`
	PaidAt          time.Time      `json:"paid_at"`
	RawNotify       map[string]any `json:"raw_notify"`
}

// Producer 支付确认投递器（notify 回调 → critical 队列，幂等键 confirm:<trade_no>）。
type Producer struct {
	Enq task.TaskEnqueuer
}

func (p *Producer) SendConfirm(ctx context.Context, cp *ConfirmPayload) error {
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	t := task.Task{
		Type: task.TaskPaymentConfirm, Queue: task.QueueCritical,
		Key: "confirm:" + cp.ProviderTradeNo, Payload: raw, Timeout: 30 * time.Second,
	}
	if err := p.Enq.Enqueue(ctx, t); err != nil {
		return fmt.Errorf("payment:confirm 投递失败: %w", err)
	}
	return nil
}

// CacheReset 入账后使余额缓存失效（否则用户充值后立即调用会被预扣读陈旧余额误拒）。
type CacheReset interface {
	ResetBalanceCache(ctx context.Context, userID int64) error
}

// CacheResetFunc 函数适配器（main 装配传闭包即可）。
type CacheResetFunc func(ctx context.Context, userID int64) error

func (f CacheResetFunc) ResetBalanceCache(ctx context.Context, userID int64) error {
	return f(ctx, userID)
}

// ConfirmEngine payment:confirm worker handler（worker 进程注册，经 task.RegisterHandler）。
type ConfirmEngine struct {
	Repo *repository.Repository
	// Cache 可选（未启用 Redis 余额缓存时为 nil）。
	Cache CacheReset
}

// Register 注册 payment:confirm handler（与 billing Engine.Register 同装配点调用）。
func (e *ConfirmEngine) Register() error {
	return task.RegisterHandler(task.TaskPaymentConfirm, e.HandleConfirm)
}

// HandleConfirm 处理 payment:confirm：幂等入账 + L2 结果缓存 + 余额缓存失效。
func (e *ConfirmEngine) HandleConfirm(ctx context.Context, t task.Task) error {
	var p ConfirmPayload
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return fmt.Errorf("payment:confirm 负载非法: %w", err)
	}
	applied, err := e.Repo.ConfirmPayment(ctx, repository.ConfirmInput{
		OrderNo: p.OrderNo, ProviderTradeNo: p.ProviderTradeNo,
		PaidAt: p.PaidAt, RawNotify: p.RawNotify,
	})
	if err != nil {
		return err // 重试由 task 层负责；ErrOrderState（状态冲突）会耗尽重试进死信告警
	}
	if !applied {
		return nil // 重复回调/已完成：幂等成功
	}
	// 成功后写 L2 幂等（失败不阻塞——L3 由 CAS+唯一索引兜底）
	_ = e.Repo.PutIdempotency(ctx, "payconfirm", p.ProviderTradeNo,
		map[string]any{"order_no": p.OrderNo}, 24*time.Hour)
	if e.Cache != nil {
		// 入账后需删除该用户余额缓存；失败仅日志（最长由缓存 TTL/下次预扣回源覆盖）
		var uid int64
		if o, err := e.Repo.PaymentOrderByNo(ctx, p.OrderNo); err == nil && o != nil {
			uid = o.UserID
		}
		if uid > 0 {
			_ = e.Cache.ResetBalanceCache(ctx, uid)
		}
	}
	return nil
}
