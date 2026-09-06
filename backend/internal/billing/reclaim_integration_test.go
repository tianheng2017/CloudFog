//go:build integration

// Reclaim（reserve:reclaim 死信补偿，06 §3.3 L3）真实 Redis+PG 集成。
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	"cloudfog/internal/repository"
)

// forceAuditAge 改写审计键为旧时间（模拟"预扣成功但结算任务丢失"超 45min 的孤儿冻结）。
func forceAuditAge(t *testing.T, cli *redis.Client, requestID string, uid int64, amount string, ago time.Duration) {
	t.Helper()
	rec, _ := json.Marshal(auditRec{UserID: uid, Amount: amount, At: time.Now().Add(-ago).Unix()})
	if err := cli.Set(context.Background(), "frozen:"+requestID, rec, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestReclaimReleasesOrphanedReserve(t *testing.T) {
	db := openBillDB(t)
	ctx := context.Background()
	s := seedBillUser(t, db, "recl-orphan")

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
	rid := fmt.Sprintf("orph-%d", time.Now().UnixNano())
	rid2 := fmt.Sprintf("chk-%d", time.Now().UnixNano())
	bkey := fmt.Sprintf("balance:%d", uid)
	t.Cleanup(func() {
		_ = cli.Del(ctx, bkey).Err()
		_ = cli.Del(ctx, "frozen:"+rid, "frozen:"+rid2).Err()
	})

	// 预扣 3 → 缓存 avail=7 frozen=3
	if err := resv.Reserve(ctx, uid, rid, decimal.NewFromInt(3)); err != nil {
		t.Fatalf("预扣失败: %v", err)
	}
	// 模拟结算任务丢失：审计键超期
	forceAuditAge(t, cli, rid, uid, "3", 60*time.Minute)

	n, err := resv.Reclaim(ctx, 45*time.Minute)
	if err != nil {
		t.Fatalf("reclaim 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应处理 1 条孤儿冻结, got %d", n)
	}
	avail, _ := cli.HGet(ctx, bkey, "avail").Result()
	frozen, _ := cli.HGet(ctx, bkey, "frozen").Result()
	if avail != "10" || frozen != "0" {
		t.Fatalf("冻结应归还 avail=10 frozen=0, got avail=%s frozen=%s", avail, frozen)
	}
	if ex, _ := cli.Exists(ctx, "frozen:"+rid).Result(); ex != 0 {
		t.Fatal("审计键应已清理")
	}
	// 归还后按缓存余额可继续预扣（余额未被虚增：上限即 PG 余额 10）
	if err := resv.Reserve(ctx, uid, rid2, decimal.NewFromInt(5)); err != nil {
		t.Fatalf("归还后预扣 5 应成功: %v", err)
	}
}

func TestReclaimSkipsSettledReserve(t *testing.T) {
	db := openBillDB(t)
	ctx := context.Background()
	s := seedBillUser(t, db, "recl-settle")

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
	rid := fmt.Sprintf("skp-%d", time.Now().UnixNano()) // 唯一 request_id：避免跨运行 settle 幂等残留
	rid5 := fmt.Sprintf("chk-%d", time.Now().UnixNano())
	bkey := fmt.Sprintf("balance:%d", uid)
	t.Cleanup(func() {
		_ = cli.Del(ctx, bkey).Err()
		_ = cli.Del(ctx, "frozen:"+rid, "frozen:"+rid5).Err()
	})

	// 预扣 2 后结算成功（余额 10 → settle 0.1 → 9.9，releaseReserve 清缓存）
	if err := resv.Reserve(ctx, uid, rid, decimal.NewFromInt(2)); err != nil {
		t.Fatalf("预扣失败: %v", err)
	}
	eng := &Engine{Repo: s.repo, Cache: resv}
	if err := eng.handleSettle(ctx, settleTask(SettlePayload{
		RequestID: rid, UserID: uid,
		Tokens:        Tokens{Input: 100},
		PriceSnapshot: PriceSnapshot{InputPer1K: decimal.NewFromInt(1), RateMultiplier: decimal.NewFromInt(1)},
	})); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if ex, _ := cli.Exists(ctx, bkey).Result(); ex != 0 {
		t.Fatalf("settle 后缓存键应被 releaseReserve 删除（证明先删后测残留场景成立）")
	}
	// 模拟首结者 crash 前 releaseReserve 未执行：残留过期审计键
	forceAuditAge(t, cli, rid, uid, "2", 60*time.Minute)

	n, err := resv.Reclaim(ctx, 45*time.Minute)
	if err != nil {
		t.Fatalf("reclaim 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应清理 1 条已结算审计键, got %d", n)
	}
	// 已有 settle 流水：不得再归还冻结（防重复释放虚增余额）→ 缓存键应维持删除态
	if ex, _ := cli.Exists(ctx, bkey).Result(); ex != 0 {
		t.Fatal("已结算请求不应重建余额缓存")
	}
	// 余额正确 = 9.9（PG 权威），非 10 或 10+2
	if err := resv.Reserve(ctx, uid, rid5, decimal.NewFromInt(1)); err != nil {
		t.Fatalf("按 PG 重载预扣应成功: %v", err)
	}
	// avail = PG 9.9 − 本次预扣 1 = 8.9：若 reclaim 曾重复释放虚增余额，此处会 > 8.9
	avail, _ := cli.HGet(ctx, bkey, "avail").Result()
	if avail != "8.9" {
		t.Fatalf("余额应为 8.9（9.9−1，未被重复释放虚增）: %s", avail)
	}
}
