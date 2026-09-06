//go:build integration

// B2-7 E2E 验收：进程级全栈（真实 PG+Redis+RabbitMQ + mock 上游）。
// 验证主链路闭环 M1/M3/M5：OpenAI 兼容请求 → 鉴权/预扣 → 转发 → 异步 settle/usage
// → 余额正确扣减、usage_logs 与 billing_ledger 一致；余额不足 402 且不发起上游。
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"cloudfog/internal/auth"
	"cloudfog/internal/billing"
	"cloudfog/internal/gateway"
	"cloudfog/internal/httpserver"
	"cloudfog/internal/model"
	_ "cloudfog/internal/pkg/adapter/openai" // 触发适配器 init 自注册（04 §2.1）
	"cloudfog/internal/repository"
	"cloudfog/internal/task"
)

const (
	e2eSalt     = "e2e-salt-0123456789abcdef"
	e2eAPIKey   = "sk-cf-e2e"
	e2eExchange = "cloudfog.tasks"
)

var registerOnce sync.Once

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CLOUDFOG_DSN")
	if dsn == "" {
		t.Skip("CLOUDFOG_DSN 未设置")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{NowFunc: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

func TestB2EndToEnd(t *testing.T) {
	url := os.Getenv("CLOUDFOG_RABBITMQ_URL")
	if url == "" {
		t.Skip("CLOUDFOG_RABBITMQ_URL 未设置")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := openDB(t)
	repo := repository.New(db)
	cli := redis.NewClient(&redis.Options{Addr: os.Getenv("CLOUDFOG_REDIS_ADDR")})
	defer cli.Close()
	if cli.Ping(ctx).Err() != nil {
		t.Skip("Redis 不可用")
	}
	reserve := billing.NewReserve(cli, repo)

	// mock 上游（OpenAI 兼容非流完成，带 usage）
	var upstreamHits int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-mock" {
			http.Error(w, `{"error":{"message":"bad upstream auth"}}`, http.StatusUnauthorized)
			return
		}
		upstreamHits++
		_, _ = io.WriteString(w, `{"id":"cmpl-mock","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"你好 E2E"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":200}}`)
	}))
	defer mock.Close()

	// ── 数据种子 ──
	uid, gid := seedUserGroup(t, db, "e2e", decimal.NewFromInt(100))
	uidEmpty, gidEmpty := seedUserGroup(t, db, "e2e-empty", decimal.Zero)
	seedKeyChannelModel(t, db, repo, uid, gid, mock.URL)
	// 空余额用户自己的密钥（不同 token 才能鉴权到该用户）
	emptyKey := &model.APIKey{
		UserID: uidEmpty, GroupID: gidEmpty, Name: "e2e-empty", KeyPrefix: "sk-cf-empty",
		KeyHash: auth.HashKey(e2eSalt, "sk-cf-e2e-empty"), Status: "active",
	}
	if err := db.Create(emptyKey).Error; err != nil {
		t.Fatalf("seed empty key: %v", err)
	}

	policy := task.RetryPolicy{RetryBuckets: []time.Duration{time.Second}, DelayBuckets: []time.Duration{time.Second}}

	// engine + worker（一次性注册/启动）
	var engine *billing.Engine
	var enq *task.RabbitEnqueuer
	registerOnce.Do(func() {
		engine = &billing.Engine{Repo: repo, Cache: reserve}
		if err := engine.Register(); err != nil {
			t.Fatalf("register engine: %v", err)
		}
	})
	enq, err := task.NewRabbitEnqueuer(ctx, url, e2eExchange, policy, task.DefaultRegistry)
	if err != nil {
		t.Fatalf("connect rabbit: %v", err)
	}
	defer func() { _ = enq.Close() }()
	if err := task.EnsureBrokerTopology(ctx, url, e2eExchange, policy, task.DefaultRegistry); err != nil {
		t.Fatalf("topology: %v", err)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = task.RunConsumers(workerCtx, task.ConsumerOptions{
			URL: url, Exchange: e2eExchange, Policy: policy, Registry: task.DefaultRegistry,
			Consumers: map[task.QueueName]task.ConsumerConfig{
				task.QueueCritical: {Enabled: true, Concurrency: 1, Prefetch: 10, MaxRetry: 3, Timeout: 30 * time.Second},
				task.QueueDefault:  {Enabled: true, Concurrency: 1, Prefetch: 10, MaxRetry: 3, Timeout: 30 * time.Second},
			},
			Handlers:        task.Handlers(),
			ShutdownTimeout: 5 * time.Second,
		})
	}()

	// API 层（gin + 网关 + 计量投递）
	gw := &gateway.Gateway{Cat: repo, Bal: repo, List: repo, Res: reserve,
		Prod: &billing.Producer{Enq: enq}}
	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	_ = eng.SetTrustedProxies(nil)
	(&httpserver.API{Store: repo, Salt: e2eSalt, Gw: gw}).Register(eng)
	srv := httptest.NewServer(eng)
	defer srv.Close()

	do := func(token string) *http.Response {
		body := `{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}`
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("chat request: %v", err)
		}
		return resp
	}

	t.Run("成功结算主链路", func(t *testing.T) {
		resp := do(e2eAPIKey)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("chat status = %d body=%s", resp.StatusCode, raw)
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 || out.Choices[0].Message.Content != "你好 E2E" {
			t.Fatalf("响应解码错误: %v %s", err, raw)
		}
		if upstreamHits == 0 {
			t.Fatal("应命中 mock 上游")
		}
		// 等待异步结算收敛：usage_logs ≥1 且 ledger settle ≥1
		waitFor(t, 15*time.Second, func() bool {
			var usageN, settleN int64
			_ = db.Model(&model.UsageLog{}).Where("user_id = ?", uid).Count(&usageN).Error
			_ = db.Model(&model.BillingLedger{}).Where("user_id = ? AND type = 'settle'", uid).Count(&settleN).Error
			return usageN >= 1 && settleN >= 1
		})
		bal, _ := repo.BalanceByUserID(ctx, uid)
		// 1000 in ×0.01/1k + 200 out ×0.03/1k = 0.01 + 0.006 = 0.016
		want := decimal.NewFromFloat(100).Sub(decimal.NewFromFloat(0.016))
		if !bal.Balance.Equal(want) {
			t.Fatalf("余额应为 %s, got %s", want, bal.Balance)
		}
		// M3：usage_logs.total_cost 与 billing_ledger.settle 一致
		var usageCost string
		var ledgerCost string
		_ = db.Raw("SELECT SUM(total_cost)::text FROM usage_logs WHERE user_id = ?", uid).Scan(&usageCost).Error
		_ = db.Raw("SELECT SUM(-amount)::text FROM billing_ledger WHERE user_id = ? AND type='settle'", uid).Scan(&ledgerCost).Error
		uc, _ := decimal.NewFromString(usageCost)
		lc, _ := decimal.NewFromString(ledgerCost)
		if !uc.Equal(lc) {
			t.Fatalf("M3 不一致: usage=%s ledger=%s", uc, lc)
		}
	})

	t.Run("余额不足 402 且不发上游", func(t *testing.T) {
		hits := upstreamHits
		resp := do("sk-cf-e2e-empty")
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusPaymentRequired {
			t.Fatalf("期望 402, got %d body=%s", resp.StatusCode, raw)
		}
		if upstreamHits != hits {
			t.Fatal("402 场景不得发起上游请求（M5）")
		}
	})

	stopWorker()
	<-workerDone
}

func seedUserGroup(t *testing.T, db *gorm.DB, tag string, bal decimal.Decimal) (int64, int64) {
	t.Helper()
	now := time.Now().UnixNano()
	g := &model.Group{Name: fmt.Sprintf("grp-%s-%d", tag, now), Status: "active"}
	if err := db.Create(g).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	u := &model.User{Email: fmt.Sprintf("%s-%d@cf.local", tag, now), Username: fmt.Sprintf("%s-%d", tag, now), Status: "active", Role: "user"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&model.UserBalance{UserID: u.ID, Balance: model.Decimal{Decimal: bal}}).Error; err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	return u.ID, g.ID
}

// seedKeyChannelModel 幂等种子（共享 dev DB 上可重复运行）：
// provider/model 复用 bootstrap 内建行，channel 以 BaseURL 覆盖指向本测试的 mock 上游。
func seedKeyChannelModel(t *testing.T, db *gorm.DB, _ *repository.Repository, uid, gid int64, base string) {
	t.Helper()
	// 幂等：清理上次运行的测试 key（key_hash 唯一，dev DB 持久）
	if err := db.Exec(`DELETE FROM api_keys WHERE key_prefix IN ('sk-cf-e2e','sk-cf-empty')`).Error; err != nil {
		t.Fatalf("clean old keys: %v", err)
	}
	key := &model.APIKey{
		UserID: uid, GroupID: gid, Name: "e2e", KeyPrefix: "sk-cf-e2e",
		KeyHash: auth.HashKey(e2eSalt, e2eAPIKey), Status: "active",
	}
	if err := db.Create(key).Error; err != nil {
		t.Fatalf("seed key: %v", err)
	}
	// provider 复用（内置 openai 种子存在；缺失则创建）
	var prov model.Provider
	if err := db.Where("code = ?", "openai").First(&prov).Error; err != nil {
		prov = model.Provider{Code: "openai", Name: "openai", Protocol: "openai_compat", BaseURL: base, AuthType: "bearer", Status: "active", Capabilities: []string{"stream"}}
		if err := db.Create(&prov).Error; err != nil {
			t.Fatalf("seed provider: %v", err)
		}
	}
	// 渠道覆盖 BaseURL → mock；凭证仅内存使用（dev 明文口径）
	ch := &model.Channel{Name: fmt.Sprintf("e2e-openai-%d", time.Now().UnixNano()), ProviderID: prov.ID, ProviderCode: prov.Code,
		BaseURL: &base, Status: "active", Schedulable: true, Credentials: map[string]any{"api_key": "sk-mock"}, Priority: 1}
	if err := db.Create(ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if err := db.Create(&model.ChannelGroup{ChannelID: ch.ID, GroupID: gid}).Error; err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	// 模型复用（存在用现成，否则创建）
	var m model.Model
	if err := db.Where("name = ?", "gpt-4o").First(&m).Error; err != nil {
		m = model.Model{Name: "gpt-4o", Status: "active", BillingMode: "token", ProviderCode: prov.Code}
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed model: %v", err)
		}
	}
	// 价格幂等：取现价覆盖为测试值（本测试独占模型的定价断言）
	var price model.ModelPrice
	if err := db.Where("model_id = ?", m.ID).Order("effective_from DESC").First(&price).Error; err != nil {
		price = model.ModelPrice{ModelID: m.ID, EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Currency: "USD",
			InputPricePer1K:  model.Decimal{Decimal: decimal.NewFromFloat(0.01)},
			OutputPricePer1K: model.Decimal{Decimal: decimal.NewFromFloat(0.03)}}
		if err := db.Create(&price).Error; err != nil {
			t.Fatalf("seed price: %v", err)
		}
	} else {
		price.InputPricePer1K = model.Decimal{Decimal: decimal.NewFromFloat(0.01)}
		price.OutputPricePer1K = model.Decimal{Decimal: decimal.NewFromFloat(0.03)}
		if err := db.Save(&price).Error; err != nil {
			t.Fatalf("update price: %v", err)
		}
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}
