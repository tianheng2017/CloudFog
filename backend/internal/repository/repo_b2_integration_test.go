//go:build integration

// B2-1 repository 资金/幂等/usage/鉴权/渠道候选集成测试。
// 需要真实 PG（表结构已由迁移建好），与 task/smoke_test.go 同模式：
//   - CI：test job 已跑过迁移门禁，本测试用 job env CLOUDFOG_DSN 连接；
//   - 本地：先 cloudfog migrate up，再 go test -tags=integration ./internal/repository/。
package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"cloudfog/internal/model"
)

func openRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CLOUDFOG_DSN")
	if dsn == "" {
		t.Skip("CLOUDFOG_DSN 未设置（需要真实 PG）")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("连接 PG 失败: %v", err)
	}
	return db
}

func atTime(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// seeds 每个测试独立命名，避免并行/重复运行的唯一键冲突。
type b2Seeds struct {
	group *model.Group
	user  *model.User
	key   *model.APIKey
}

func seedB2(t *testing.T, db *gorm.DB, tag string) *b2Seeds {
	t.Helper()
	now := time.Now().UnixNano()
	s := &b2Seeds{}
	g := &model.Group{Name: fmt.Sprintf("itg-%s-%d", tag, now), Status: "active"}
	if err := db.Create(g).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	s.group = g
	u := &model.User{
		Email:    fmt.Sprintf("it-%s-%d@cloudfog.local", tag, now),
		Username: fmt.Sprintf("it-%s-%d", tag, now),
		Status:   "active",
		Role:     "user",
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	s.user = u
	key := &model.APIKey{
		UserID:    u.ID,
		GroupID:   g.ID,
		Name:      "integration",
		KeyPrefix: "sk-it",
		KeyHash:   fmt.Sprintf("%064d", now), // 64 位测试哈希
		Status:    "active",
	}
	if err := db.Create(key).Error; err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	s.key = key
	t.Cleanup(func() {
		// 逆序清理（约束）。usage_logs 为只追加且无 FK，不做清理（每次运行留一条测试行）。
		_ = db.Unscoped().Delete(&model.APIKey{}, key.ID).Error
		// 先清 user 的余额/流水行（user_balances 无 FK、billing_ledger 不级联），
		// 避免删除 user 后残留孤儿行（本地反复运行的卫生；DDL 侧无约束兜底，运维注意）。
		_ = db.Exec("DELETE FROM user_balances WHERE user_id = ?", u.ID).Error
		_ = db.Exec("DELETE FROM billing_ledger WHERE user_id = ?", u.ID).Error
		_ = db.Unscoped().Delete(&model.User{}, u.ID).Error
		_ = db.Unscoped().Delete(&model.Group{}, g.ID).Error
	})
	return s
}

func TestB2APIKeyByHash(t *testing.T) {
	db := openRepoTestDB(t)
	repo := New(db)
	s := seedB2(t, db, "auth")

	got, err := repo.APIKeyByHash(context.Background(), s.key.KeyHash)
	if err != nil {
		t.Fatalf("APIKeyByHash: %v", err)
	}
	if got == nil || got.ID != s.key.ID {
		t.Fatalf("按哈希应命中 key %d, got %+v", s.key.ID, got)
	}
	miss, err := repo.APIKeyByHash(context.Background(), fmt.Sprintf("%064d", time.Now().UnixNano()))
	if err != nil || miss != nil {
		t.Fatalf("未知哈希应返回 (nil,nil), got (%+v, %v)", miss, err)
	}
}

func TestB2BalancePrimitives(t *testing.T) {
	db := openRepoTestDB(t)
	repo := New(db)
	ctx := context.Background()
	s := seedB2(t, db, "bal")

	if err := repo.EnsureBalance(ctx, s.user.ID); err != nil {
		t.Fatalf("EnsureBalance: %v", err)
	}
	// 幂等：二次 Ensure 不报错
	if err := repo.EnsureBalance(ctx, s.user.ID); err != nil {
		t.Fatalf("EnsureBalance 二次: %v", err)
	}
	b, err := repo.BalanceByUserID(ctx, s.user.ID)
	if err != nil || b == nil {
		t.Fatalf("BalanceByUserID: (%+v, %v)", b, err)
	}
	if !b.Balance.Equal(decimal.Zero) || !b.Frozen.Equal(decimal.Zero) {
		t.Fatalf("新余额行应为 0, got balance=%s frozen=%s", b.Balance, b.Frozen)
	}

	// 充值 100：Balance +100
	ok, err := repo.ApplyBalanceDelta(ctx, s.user.ID, BalanceDelta{Balance: decimal.NewFromInt(100)})
	if err != nil || !ok {
		t.Fatalf("充值应 applied, got (%v, %v)", ok, err)
	}
	// 冻结 30：Balance -30 / Frozen +30
	ok, err = repo.ApplyBalanceDelta(ctx, s.user.ID, BalanceDelta{Balance: decimal.NewFromInt(-30), Frozen: decimal.NewFromInt(30)})
	if err != nil || !ok {
		t.Fatalf("冻结应 applied, got (%v, %v)", ok, err)
	}
	b, _ = repo.BalanceByUserID(ctx, s.user.ID)
	if !b.Balance.Equal(decimal.NewFromInt(70)) || !b.Frozen.Equal(decimal.NewFromInt(30)) {
		t.Fatalf("期望 70/30, got balance=%s frozen=%s", b.Balance, b.Frozen)
	}
	// 超额扣减（> 余额）必须被 DB 条件拒绝：不 applied、余额不变
	ok, err = repo.ApplyBalanceDelta(ctx, s.user.ID, BalanceDelta{Balance: decimal.NewFromInt(-200)})
	if err != nil || ok {
		t.Fatalf("超额扣减应 !applied 且无错, got (%v, %v)", ok, err)
	}
	b, _ = repo.BalanceByUserID(ctx, s.user.ID)
	if !b.Balance.Equal(decimal.NewFromInt(70)) {
		t.Fatalf("超额扣减后余额应不变 70, got %s", b.Balance)
	}
}

func TestB2IdempotencyLifecycle(t *testing.T) {
	db := openRepoTestDB(t)
	repo := New(db)
	ctx := context.Background()
	key := fmt.Sprintf("it-dup-%d", time.Now().UnixNano())

	// 未命中
	if rec, err := repo.GetIdempotency(ctx, "settle", key); err != nil || rec != nil {
		t.Fatalf("初始应未命中, got (%+v, %v)", rec, err)
	}
	// 写入 + 二次覆盖（唯一约束 upsert 语义，06 §10.2）
	if err := repo.PutIdempotency(ctx, "settle", key, map[string]any{"ok": true}, time.Hour); err != nil {
		t.Fatalf("PutIdempotency: %v", err)
	}
	if err := repo.PutIdempotency(ctx, "settle", key, map[string]any{"ok": true, "again": 1}, time.Hour); err != nil {
		t.Fatalf("PutIdempotency 覆盖: %v", err)
	}
	rec, err := repo.GetIdempotency(ctx, "settle", key)
	if err != nil || rec == nil {
		t.Fatalf("写入后应命中, got (%+v, %v)", rec, err)
	}
	if rec.Result["again"] != float64(1) {
		t.Fatalf("应取到覆盖结果, got %v", rec.Result)
	}
}

func TestB2InsertUsageLog(t *testing.T) {
	db := openRepoTestDB(t)
	repo := New(db)
	ctx := context.Background()
	s := seedB2(t, db, "usage")

	l := &model.UsageLog{
		RequestID:     fmt.Sprintf("req-%d", time.Now().UnixNano()),
		UserID:        s.user.ID,
		APIKeyID:      s.key.ID,
		ChannelID:     1,
		Model:         "gpt-4o",
		ProviderCode:  "openai",
		InputTokens:   10,
		OutputTokens:  5,
		TotalCost:     model.Decimal{Decimal: decimal.NewFromInt(1).Div(decimal.NewFromInt(1000))},
		PriceSnapshot: map[string]any{"input_per_1k": "0.01"},
		StatusCode:    200,
	}
	if err := repo.InsertUsageLog(ctx, l); err != nil {
		t.Fatalf("InsertUsageLog: %v", err)
	}
	if l.ID == 0 {
		t.Fatal("usage_logs 应返回自增 id")
	}
}

func TestB2RouteCandidatesByGroup(t *testing.T) {
	db := openRepoTestDB(t)
	repo := New(db)
	ctx := context.Background()
	tag := fmt.Sprintf("route-%d", time.Now().UnixNano())
	s := seedB2(t, db, tag)

	// 种子：两家供应商各一个渠道，仅第一个加入该分组；再建一个 disabled 渠道验证被过滤。
	provA := &model.Provider{Code: "it-" + tag + "-a", Name: "A", Protocol: "openai_compat", BaseURL: "https://a.invalid", AuthType: "bearer", Status: "active"}
	provB := &model.Provider{Code: "it-" + tag + "-b", Name: "B", Protocol: "anthropic", BaseURL: "https://b.invalid", AuthType: "bearer", Status: "active"}
	if err := db.Create(provA).Error; err != nil {
		t.Fatalf("seed provA: %v", err)
	}
	if err := db.Create(provB).Error; err != nil {
		t.Fatalf("seed provB: %v", err)
	}
	chIn := &model.Channel{Name: tag + "-in", ProviderID: provA.ID, ProviderCode: provA.Code, Priority: 10, Weight: 100, Status: "active", Schedulable: true, Credentials: map[string]any{"api_key": "x"}}
	chOut := &model.Channel{Name: tag + "-out", ProviderID: provB.ID, ProviderCode: provB.Code, Priority: 5, Weight: 50, Status: "active", Schedulable: true, Credentials: map[string]any{}}
	if err := db.Create(chIn).Error; err != nil {
		t.Fatalf("seed chIn: %v", err)
	}
	if err := db.Create(chOut).Error; err != nil {
		t.Fatalf("seed chOut: %v", err)
	}
	m := &model.ChannelGroup{ChannelID: chIn.ID, GroupID: s.group.ID}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM channel_groups WHERE channel_id = ?", chIn.ID).Error
		_ = db.Unscoped().Delete(&model.Channel{}, chIn.ID).Error
		_ = db.Unscoped().Delete(&model.Channel{}, chOut.ID).Error
		_ = db.Unscoped().Delete(&model.Provider{}, provA.ID).Error
		_ = db.Unscoped().Delete(&model.Provider{}, provB.ID).Error
	})

	cands, err := repo.RouteCandidatesByGroup(ctx, s.group.ID)
	if err != nil {
		t.Fatalf("RouteCandidatesByGroup: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("应恰有 1 个候选（未加入分组的渠道不可见），got %d", len(cands))
	}
	if cands[0].Channel.ID != chIn.ID || cands[0].Provider.Code != provA.Code {
		t.Fatalf("候选归属错误: channel=%d provider=%s", cands[0].Channel.ID, cands[0].Provider.Code)
	}
}

func TestB2ModelCatalog(t *testing.T) {
	db := openRepoTestDB(t)
	repo := New(db)
	ctx := context.Background()
	tag := fmt.Sprintf("cat-%d", time.Now().UnixNano())

	m := &model.Model{Name: "it-" + tag, DisplayName: "it", Status: "active", BillingMode: "token"}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("seed model: %v", err)
	}
	past := atTime(2026, 8, 1)
	recent := atTime(2026, 9, 1)
	future := atTime(2026, 10, 1)
	for _, eff := range []time.Time{past, recent, future} {
		mp := &model.ModelPrice{ModelID: m.ID, EffectiveFrom: eff, Currency: "USD"}
		if err := db.Create(mp).Error; err != nil {
			t.Fatalf("seed price %v: %v", eff, err)
		}
	}
	mg := &model.ModelMapping{Alias: "it-" + tag, UpstreamModel: "up-" + tag} // 全局映射
	if err := db.Create(mg).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM model_mappings WHERE id = ?", mg.ID).Error
		_ = db.Exec("DELETE FROM model_prices WHERE model_id = ?", m.ID).Error
		_ = db.Unscoped().Delete(&model.Model{}, m.ID).Error
	})

	got, err := repo.ModelByName(ctx, m.Name)
	if err != nil || got == nil || got.ID != m.ID {
		t.Fatalf("ModelByName: (%+v, %v)", got, err)
	}
	miss, err := repo.ModelByName(ctx, "no-such-"+tag)
	if err != nil || miss != nil {
		t.Fatalf("未知模型应 (nil,nil): (%+v, %v)", miss, err)
	}
	// 现时（09-06）应取 09-01（最近生效），不含未来档
	now := atTime(2026, 9, 6)
	p, err := repo.CurrentModelPrice(ctx, m.ID, now)
	if err != nil || p == nil || !p.EffectiveFrom.Equal(recent) {
		t.Fatalf("CurrentModelPrice 应取最近生效(09-01), got eff=%v err=%v", p.EffectiveFrom, err)
	}
	rows, err := repo.MappingRows(ctx, mg.Alias)
	if err != nil || len(rows) != 1 || rows[0].UpstreamModel != "up-"+tag {
		t.Fatalf("MappingRows: rows=%+v err=%v", rows, err)
	}
}

func TestB2LedgerWriteAndExists(t *testing.T) {
	db := openRepoTestDB(t)
	repo := New(db)
	ctx := context.Background()
	s := seedB2(t, db, "ledger")
	if err := repo.EnsureBalance(ctx, s.user.ID); err != nil {
		t.Fatal(err)
	}
	reqID := fmt.Sprintf("req-lgr-%d", time.Now().UnixNano())
	l := &model.BillingLedger{
		UserID: s.user.ID, Type: "settle", RequestID: reqID,
		Amount:       model.Decimal{Decimal: decimal.NewFromInt(-1)},
		BalanceAfter: model.Decimal{Decimal: decimal.NewFromInt(99)},
		Description:  "integration",
	}
	if err := repo.InsertLedger(ctx, l); err != nil {
		t.Fatalf("InsertLedger: %v", err)
	}
	if l.ID == 0 {
		t.Fatal("ledger 应返回自增 id")
	}
	exists, err := repo.LedgerExistsByRequestID(ctx, reqID)
	if err != nil || !exists {
		t.Fatalf("LedgerExistsByRequestID 应 true: (%v, %v)", exists, err)
	}
	if exists, _ := repo.LedgerExistsByRequestID(ctx, "no-such-req"); exists {
		t.Fatal("未知 request_id 应 false")
	}
	_ = db.Exec("DELETE FROM billing_ledger WHERE id = ?", l.ID)
}
