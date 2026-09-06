//go:build integration

// Reserve（Redis 原子预扣）真实环境集成：预扣/不足/引擎结算后重置重载。
// 需要 CLOUDFOG_REDIS_ADDR（默认 127.0.0.1:6379）；Redis 不可用则 skip。
package billing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	"cloudfog/internal/repository"
)

func redisAddr() string {
	if v := os.Getenv("CLOUDFOG_REDIS_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:6379"
}

func TestReserveLifecycle(t *testing.T) {
	db := openBillDB(t)
	ctx := context.Background()
	s := seedBillUser(t, db, "resv")

	cli := redis.NewClient(&redis.Options{Addr: redisAddr()})
	defer cli.Close()
	if err := cli.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis 不可用，跳过: %v", err)
	}
	if ok, _ := s.repo.ApplyBalanceDelta(ctx, s.user.ID, repository.BalanceDelta{Balance: decimal.NewFromInt(10)}); !ok {
		t.Fatal("充值失败")
	}
	resv := NewReserve(cli, s.repo)
	uid := s.user.ID
	t.Cleanup(func() {
		_ = cli.Del(ctx, fmt.Sprintf("balance:%d", uid)).Err()
		_ = cli.Del(ctx, "frozen:r1", "frozen:r2", "frozen:r3").Err()
	})

	// 预扣成功（缓存未加载→回源→重试）
	if err := resv.Reserve(ctx, uid, "r1", decimal.NewFromInt(3)); err != nil {
		t.Fatalf("预扣失败: %v", err)
	}
	// 余额不足
	if err := resv.Reserve(ctx, uid, "r2", decimal.NewFromInt(999)); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("应 ErrInsufficient, got %v", err)
	}
	// 引擎结算（Cache 挂接）后：缓存被重置，下次预扣从 PG 重载（余额 10→扣 1 =9）
	eng := &Engine{Repo: s.repo, Cache: resv}
	if err := eng.handleSettle(ctx, settleTask(SettlePayload{
		RequestID: "r1", UserID: uid,
		Tokens:        Tokens{Input: 100},
		PriceSnapshot: PriceSnapshot{InputPer1K: decimal.NewFromInt(1), RateMultiplier: decimal.NewFromInt(1)},
	})); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	// 结算后余额应为 9；再次预扣 5 成功、再预扣 5 不足
	if err := resv.Reserve(ctx, uid, "r3", decimal.NewFromInt(5)); err != nil {
		t.Fatalf("预扣 5 应成功（缓存已重置重载）: %v", err)
	}
	if err := resv.Reserve(ctx, uid, "r2", decimal.NewFromInt(6)); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("预扣 6 应不足, got %v", err)
	}
}
