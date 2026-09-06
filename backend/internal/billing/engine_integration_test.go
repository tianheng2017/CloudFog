//go:build integration

// billing 引擎真实 PG 集成：结算/重复幂等/usage 写入/退款/欠费零额分支。
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"cloudfog/internal/model"
	"cloudfog/internal/repository"
	"cloudfog/internal/task"
)

func openBillDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CLOUDFOG_DSN")
	if dsn == "" {
		t.Skip("CLOUDFOG_DSN 未设置（需要真实 PG）")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{NowFunc: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		t.Fatalf("连接 PG 失败: %v", err)
	}
	return db
}

type billSeed struct {
	user *model.User
	repo *repository.Repository
}

func seedBillUser(t *testing.T, db *gorm.DB, tag string) *billSeed {
	t.Helper()
	now := time.Now().UnixNano()
	u := &model.User{Email: fmt.Sprintf("bill-%s-%d@cf.local", tag, now), Username: fmt.Sprintf("bill-%s-%d", tag, now), Status: "active", Role: "user"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	repo := repository.New(db)
	if err := repo.EnsureBalance(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM user_balances WHERE user_id = ?", u.ID).Error
		_ = db.Exec("DELETE FROM billing_ledger WHERE user_id = ?", u.ID).Error
		_ = db.Unscoped().Delete(&model.User{}, u.ID).Error
	})
	return &billSeed{user: u, repo: repo}
}

func settleTask(p SettlePayload) task.Task {
	b, _ := json.Marshal(p)
	return task.Task{Type: task.TaskBillingSettle, Queue: task.QueueCritical, Payload: b, Timeout: time.Second}
}

func TestEngineSettleIdempotent(t *testing.T) {
	db := openBillDB(t)
	ctx := context.Background()
	s := seedBillUser(t, db, "settle")
	eng := &Engine{Repo: s.repo}

	if ok, err := s.repo.ApplyBalanceDelta(ctx, s.user.ID, repository.BalanceDelta{Balance: decimal.NewFromInt(100)}); err != nil || !ok {
		t.Fatalf("充值失败: %v %v", ok, err)
	}
	p := SettlePayload{
		RequestID:     fmt.Sprintf("req-settle-%d", time.Now().UnixNano()),
		UserID:        s.user.ID,
		Tokens:        Tokens{Input: 1000, Output: 0},
		PriceSnapshot: PriceSnapshot{InputPer1K: decimal.NewFromFloat(0.01), RateMultiplier: decimal.NewFromInt(1)},
	}
	if err := eng.handleSettle(ctx, settleTask(p)); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	bal, _ := s.repo.BalanceByUserID(ctx, s.user.ID)
	if !bal.Balance.Equal(decimal.NewFromFloat(99.99)) {
		t.Fatalf("结算后余额应为 99.99, got %s", bal.Balance)
	}
	// 重复投递同一 request：L2 命中，不得二次扣费
	if err := eng.handleSettle(ctx, settleTask(p)); err != nil {
		t.Fatalf("重复结算失败: %v", err)
	}
	bal, _ = s.repo.BalanceByUserID(ctx, s.user.ID)
	if !bal.Balance.Equal(decimal.NewFromFloat(99.99)) {
		t.Fatalf("重复结算不得二次扣费, got %s", bal.Balance)
	}
}

func TestEngineUsageWriteIdempotent(t *testing.T) {
	db := openBillDB(t)
	ctx := context.Background()
	s := seedBillUser(t, db, "usage")
	eng := &Engine{Repo: s.repo}
	up := UsageLogPayload{
		RequestID:    fmt.Sprintf("req-usage-%d", time.Now().UnixNano()),
		UserID:       s.user.ID,
		ChannelID:    1,
		Model:        "gpt-4o",
		ProviderCode: "openai",
		Tokens:       Tokens{Input: 10},
	}
	b, _ := json.Marshal(up)
	tt := task.Task{Type: task.TaskUsageWrite, Queue: task.QueueDefault, Payload: b, Timeout: time.Second}
	if err := eng.handleUsageWrite(ctx, tt); err != nil {
		t.Fatalf("usage 写入失败: %v", err)
	}
	if err := eng.handleUsageWrite(ctx, tt); err != nil {
		t.Fatalf("usage 重复写失败: %v", err)
	}
	var n int64
	if err := db.Model(&model.UsageLog{}).Where("request_id = ?", up.RequestID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("usage_logs 应仅 1 行（幂等）, got %d", n)
	}
}

func TestEngineRefund(t *testing.T) {
	db := openBillDB(t)
	ctx := context.Background()
	s := seedBillUser(t, db, "refund")
	eng := &Engine{Repo: s.repo}
	if ok, _ := s.repo.ApplyBalanceDelta(ctx, s.user.ID, repository.BalanceDelta{Balance: decimal.NewFromInt(10)}); !ok {
		t.Fatal("充值失败")
	}
	p := RefundPayload{RequestID: fmt.Sprintf("req-refund-%d", time.Now().UnixNano()), UserID: s.user.ID, Amount: decimal.NewFromFloat(2.5)}
	b, _ := json.Marshal(p)
	if err := eng.handleRefund(ctx, task.Task{Type: task.TaskBillingRefund, Queue: task.QueueCritical, Payload: b, Timeout: time.Second}); err != nil {
		t.Fatalf("退款失败: %v", err)
	}
	bal, _ := s.repo.BalanceByUserID(ctx, s.user.ID)
	if !bal.Balance.Equal(decimal.NewFromFloat(12.5)) {
		t.Fatalf("退款后余额应为 12.5, got %s", bal.Balance)
	}
}

func TestEngineSettleOverdraft(t *testing.T) {
	db := openBillDB(t)
	ctx := context.Background()
	s := seedBillUser(t, db, "od")
	eng := &Engine{Repo: s.repo}
	if ok, _ := s.repo.ApplyBalanceDelta(ctx, s.user.ID, repository.BalanceDelta{Balance: decimal.NewFromFloat(0.01)}); !ok {
		t.Fatal("充值失败")
	}
	p := SettlePayload{
		RequestID:     fmt.Sprintf("req-od-%d", time.Now().UnixNano()),
		UserID:        s.user.ID,
		Tokens:        Tokens{Input: 100},
		PriceSnapshot: PriceSnapshot{InputPer1K: decimal.NewFromInt(10), RateMultiplier: decimal.NewFromInt(1)}, // cost=1 > 余额 0.01
	}
	if err := eng.handleSettle(ctx, settleTask(p)); err != nil {
		t.Fatalf("欠费结算不应报错: %v", err)
	}
	bal, _ := s.repo.BalanceByUserID(ctx, s.user.ID)
	if !bal.Balance.Equal(decimal.Zero) {
		t.Fatalf("欠费后余额应为 0, got %s", bal.Balance)
	}
	var l model.BillingLedger
	if err := db.Where("request_id = ?", p.RequestID).First(&l).Error; err != nil {
		t.Fatalf("应有流水: %v", err)
	}
	if !l.BalanceAfter.Equal(decimal.Zero) || l.Overdraft.LessThanOrEqual(decimal.Zero) {
		t.Fatalf("欠费流水应 balance_after=0 且 overdraft>0, got %s/%s", l.BalanceAfter, l.Overdraft)
	}
}
