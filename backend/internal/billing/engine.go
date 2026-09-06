package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"cloudfog/internal/model"
	"cloudfog/internal/repository"
	"cloudfog/internal/task"
)

// Resetter Redis 预扣状态重置（billing.Reserve 实现；nil = 未启用 Redis 预扣）。
type Resetter interface {
	Reset(ctx context.Context, userID int64, requestID string) error
}

// Engine 结算领域执行器（异步 handler 编排）。
type Engine struct {
	Repo  *repository.Repository
	Cache Resetter // 可选：提交后释放 Redis 预扣缓存（06 §3.2 结算后同步）
}

// idemTTL L2 幂等记录保留时长（idempotency:cleanup 周期清理，01 §7.2）。
const idemTTL = 24 * time.Hour

// reclaimThreshold 冻结额度补偿阈值（06 §3.3）：> settle 总重试窗口约 41min，防误释放合法重试中冻结。
const reclaimThreshold = 45 * time.Minute

// Register 向 task 注册表注入四类 handler（cmd wiring 调用）。
func (e *Engine) Register() error {
	if err := task.RegisterHandler(task.TaskUsageWrite, e.handleUsageWrite); err != nil {
		return err
	}
	if err := task.RegisterHandler(task.TaskBillingSettle, e.handleSettle); err != nil {
		return err
	}
	if err := task.RegisterHandler(task.TaskBillingRefund, e.handleRefund); err != nil {
		return err
	}
	return task.RegisterHandler(task.TaskReserveReclaim, e.handleReclaim)
}

// handleReclaim 死信补偿：扫描超阈值且无 settle 流水的冻结并归还（06 §3.3 L3 / M12）。
// 周期任务负载为空（仅触发语义）；未启用 Redis 预扣（Cache 非 Reserve）时空转。
func (e *Engine) handleReclaim(ctx context.Context, _ task.Task) error {
	rc, ok := e.Cache.(interface {
		Reclaim(ctx context.Context, olderThan time.Duration) (int, error)
	})
	if !ok || rc == nil {
		return nil // 无 Redis 预扣：无冻结额度需回收
	}
	if _, err := rc.Reclaim(ctx, reclaimThreshold); err != nil {
		return err
	}
	return nil
}

func decode[T any](t task.Task, v *T) error {
	if len(t.Payload) == 0 {
		return fmt.Errorf("task: %s 负载为空", t.Type)
	}
	if err := json.Unmarshal(t.Payload, v); err != nil {
		return fmt.Errorf("task: %s 负载解析失败: %w", t.Type, err)
	}
	return nil
}

// ── usage:write ───────────────────────────────────────────

func (e *Engine) handleUsageWrite(ctx context.Context, t task.Task) error {
	var p UsageLogPayload
	if err := decode(t, &p); err != nil {
		return err
	}
	if p.RequestID == "" || p.UserID <= 0 {
		return fmt.Errorf("task: usage:write 负载缺 request_id/user_id")
	}
	// 幂等（L2）：已写过该 request_id 直接返回
	if rec, err := e.Repo.GetIdempotency(ctx, "usage", p.RequestID); err != nil {
		return err
	} else if rec != nil {
		return nil
	}
	log := &model.UsageLog{
		RequestID:        p.RequestID,
		UserID:           p.UserID,
		APIKeyID:         p.APIKeyID,
		ChannelID:        p.ChannelID,
		GroupID:          p.GroupID,
		Model:            p.Model,
		UpstreamModel:    p.UpstreamModel,
		ProviderCode:     p.ProviderCode,
		InputTokens:      p.Tokens.Input,
		OutputTokens:     p.Tokens.Output,
		CacheReadTokens:  p.Tokens.CacheRead,
		CacheWriteTokens: p.Tokens.CacheWrite,
		TotalCost:        model.Decimal{Decimal: p.TotalCost},
		PriceSnapshot: map[string]any{
			"input_per_1k":  p.PriceSnapshot.InputPer1K.String(),
			"output_per_1k": p.PriceSnapshot.OutputPer1K.String(),
			"rate":          p.PriceSnapshot.RateMultiplier.String(),
		},
		BillingMode: "token",
		UsageSource: "upstream",
		Stream:      p.Stream,
		StatusCode:  p.StatusCode,
		ErrorCode:   p.ErrorCode,
		ClientIP:    p.ClientIP,
		UserAgent:   p.UserAgent,
	}
	if err := e.Repo.InsertUsageLog(ctx, log); err != nil {
		return err
	}
	// 成功后写 L2（06 §10.2：仅成功后写，避免挡重试）
	return e.Repo.PutIdempotency(ctx, "usage", p.RequestID, nil, idemTTL)
}

// ── billing:settle ────────────────────────────────────────

func (e *Engine) handleSettle(ctx context.Context, t task.Task) error {
	var p SettlePayload
	if err := decode(t, &p); err != nil {
		return err
	}
	if p.RequestID == "" || p.UserID <= 0 {
		return fmt.Errorf("task: billing:settle 负载缺 request_id/user_id")
	}
	key := "settle:" + p.RequestID
	if rec, err := e.Repo.GetIdempotency(ctx, "settle", key); err != nil {
		return err
	} else if rec != nil {
		return nil // L2 快速路径（重复投递）
	}
	// 金额以价格快照权威重算（06 §4.2：worker 按 price_snapshot 计算真实费用）
	amount := ComputeCost(p.PriceSnapshot, p.Tokens, false)
	if amount.IsNegative() {
		return fmt.Errorf("task: settle 金额为负: %s", amount)
	}

	if err := e.applySettleTx(ctx, p, amount); err != nil {
		return err
	}
	// L2 幂等记录 + 释放预扣。注意：duplicate（L3 撞唯一 = 他worker/重投已结算）也须写 L2 并释放——
	// 否则 L2 写失败触发的重投会反复执行完整扣减事务（撞唯一回滚），且预扣缓存键可能永久残留
	// （若首结者恰在 releaseReserve 前崩溃），下次该用户预扣将读到陈旧余额。DEL 幂等无副作用。
	if err := e.Repo.PutIdempotency(ctx, "settle", key, map[string]any{"amount": amount.String()}, idemTTL); err != nil {
		return err
	}
	e.releaseReserve(ctx, p.UserID, p.RequestID)
	return nil
}

// releaseReserve 提交后释放预扣状态（删除缓存键，下次预扣从 PG 重载）。
// 失败仅告警级（缓存不一致由重载自愈），不阻断结算结果。
func (e *Engine) releaseReserve(ctx context.Context, userID int64, requestID string) {
	if e.Cache == nil {
		return
	}
	_ = e.Cache.Reset(ctx, userID, requestID)
}

// applySettleTx 单事务结算：条件更新（含欠费零额分支）+ ledger 流水。
// L3 撞唯一（uq_ledger_settle）归化为幂等成功返回 nil——事务整体回滚保证余额不被二次扣减。
func (e *Engine) applySettleTx(ctx context.Context, p SettlePayload, amount decimal.Decimal) error {
	if amount.IsZero() {
		return nil // 零费用：不产生流水
	}
	err := e.Repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 尝试全额扣减（条件更新，06 §5.3；返回语义：扣后 balance）
		res := tx.Exec(
			`UPDATE user_balances SET balance = balance - ?, total_consumed = total_consumed + ?,
			        version = version + 1, updated_at = now()
			 WHERE user_id = ? AND balance >= ?`, amount, amount, p.UserID, amount)
		if res.Error != nil {
			return res.Error
		}
		ledger := &model.BillingLedger{UserID: p.UserID, Type: "settle", RequestID: p.RequestID, Description: "调用结算"}
		if res.RowsAffected == 1 {
			bal, err := fetchBalance(tx, p.UserID)
			if err != nil {
				return err
			}
			ledger.Amount = model.Decimal{Decimal: amount.Neg()}
			ledger.BalanceAfter = model.Decimal{Decimal: bal}
		} else {
			// 余额不足：零额分支（06 §4.3）——清零 + overdraft 记账，余额恒非负
			bal, err := fetchBalance(tx, p.UserID)
			if err != nil {
				return err
			}
			if err := tx.Exec(
				`UPDATE user_balances SET balance = 0, total_consumed = total_consumed + ?,
				        version = version + 1, updated_at = now() WHERE user_id = ?`, bal, p.UserID).Error; err != nil {
				return err
			}
			overdraft := amount.Sub(bal)
			ledger.Amount = model.Decimal{Decimal: bal.Neg()}
			ledger.Overdraft = model.Decimal{Decimal: overdraft}
			ledger.BalanceAfter = model.Decimal{Decimal: decimal.Zero}
		}
		if err := tx.Create(ledger).Error; err != nil {
			if repository.IsUniqueViolation(err) {
				return errSentinelDuplicate
			}
			return err
		}
		return nil
	})
	if errors.Is(err, errSentinelDuplicate) {
		return nil
	}
	return err
}

// errSentinelDuplicate 事务内信号：ledger 唯一约束冲突（tx 内用于回滚，tx 外归化为幂等成功）。
var errSentinelDuplicate = errors.New("billing: ledger duplicate")

// fetchBalance 读用户余额（pgx numeric → 文本 → decimal）。
func fetchBalance(tx *gorm.DB, userID int64) (decimal.Decimal, error) {
	var raw string
	if err := tx.Raw(`SELECT balance::text FROM user_balances WHERE user_id = ?`, userID).Scan(&raw).Error; err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(raw)
}

// ── billing:refund ───────────────────────────────────────

func (e *Engine) handleRefund(ctx context.Context, t task.Task) error {
	var p RefundPayload
	if err := decode(t, &p); err != nil {
		return err
	}
	if p.RequestID == "" || p.UserID <= 0 || p.Amount.IsNegative() {
		return fmt.Errorf("task: billing:refund 负载非法")
	}
	key := "refund:" + p.RequestID
	if rec, err := e.Repo.GetIdempotency(ctx, "refund", key); err != nil {
		return err
	} else if rec != nil {
		return nil
	}
	if !p.Amount.IsPositive() {
		return nil // 零退回无需流水
	}
	err := e.Repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`UPDATE user_balances SET balance = balance + ?, version = version + 1, updated_at = now()
			 WHERE user_id = ?`, p.Amount, p.UserID)
		if res.Error != nil {
			return res.Error
		}
		bal, err := fetchBalance(tx, p.UserID)
		if err != nil {
			return err
		}
		l := &model.BillingLedger{UserID: p.UserID, Type: "refund", RequestID: p.RequestID, Amount: model.Decimal{Decimal: p.Amount}, BalanceAfter: model.Decimal{Decimal: bal}, Description: "退回：" + p.Reason}
		if err := tx.Create(l).Error; err != nil {
			if repository.IsUniqueViolation(err) {
				return errSentinelDuplicate
			}
			return err
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSentinelDuplicate) {
		return err
	}
	if err := e.Repo.PutIdempotency(ctx, "refund", key, nil, idemTTL); err != nil {
		return err
	}
	e.releaseReserve(ctx, p.UserID, p.RequestID)
	return nil
}
