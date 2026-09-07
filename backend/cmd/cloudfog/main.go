// cloudfog 单二进制多职责（01 §2）：API / Worker / Scheduler 由 --role 切换；
// migrate 子命令执行数据库迁移（golang-migrate library，02 §11）。
//
// 阶段 3（B1 验收前置）：角色启动链路（配置全量校验 → 日志 → DB 连接与 schema
// 版本检查 → 角色各自运行），信号驱动优雅关闭。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"cloudfog/internal/billing"
	"cloudfog/internal/bootstrap"
	"cloudfog/internal/config"
	"cloudfog/internal/gateway"
	"cloudfog/internal/httpserver"
	"cloudfog/internal/migrate"
	"cloudfog/internal/model"
	"cloudfog/internal/payment"
	"cloudfog/internal/pkg/logger"
	"cloudfog/internal/repository"
	"cloudfog/internal/stats"
	"cloudfog/internal/task"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// schemaVersion 本版本要求的迁移基线（migrations 文件名前缀 20260907000001：auth_sessions）。
const schemaVersion uint64 = 20260907000001

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if err := config.LoadDotEnv(".env", "../.env"); err != nil {
		fmt.Fprintf(os.Stderr, "dotenv: %v\n", err)
		return 1
	}
	if len(args) == 0 {
		usage()
		return 1
	}

	switch args[0] {
	case "migrate":
		return runMigrate(args[1:])
	case "bootstrap":
		return runBootstrap()
	case "version":
		fmt.Println("cloudfog (B1 stage3)")
		return 0
	}

	role := strings.TrimPrefix(args[0], "--role=")
	if role == args[0] {
		usage()
		return 1
	}
	switch role {
	case "api", "worker", "scheduler", "all":
	default:
		fmt.Fprintf(os.Stderr, "cloudfog: 未知角色 %q（api|worker|scheduler|all）\n", role)
		usage()
		return 1
	}
	// --config=<path> 覆盖 config.yaml（10 §3.1：yaml + 环境变量双源）
	cfgPath := ""
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "--config=") {
			cfgPath = strings.TrimPrefix(a, "--config=")
			break
		}
	}
	if err := startRoles(role, cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// startRoles 加载配置并按角色启动（all = api+worker+scheduler）。
// configPath 空 = 仅默认值 + 环境变量（B1/.env 场景）。
func startRoles(role, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("启动失败: %w", err)
	}
	log := logger.New(logger.Options{Level: cfg.Observability.LogLevel, Format: cfg.Observability.LogFormat})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 通用基础设施：DB 连接 + schema 版本门禁（01 §10 启动迁移检查）
	db, closeDB, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()
	if err := migrate.RequireSchemaVersion(cfg.Database.DSN, schemaVersion); err != nil {
		return fmt.Errorf("启动失败: %w", err)
	}

	// 业务装配：repository + Redis（预扣缓存）+ billing 引擎 handler 注册（b2-6）
	repo := repository.New(db)
	rds := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: 0})
	defer func() { _ = rds.Close() }()
	reserve := billing.NewReserve(rds, repo)
	if err := (&billing.Engine{Repo: repo, Cache: reserve}).Register(); err != nil {
		return fmt.Errorf("启动失败: 注册结算 handler 失败: %w", err)
	}
	// b3-4：payment:confirm 入账引擎（worker 消费；入账后 DEL 余额缓存，防充值后预扣读陈旧）
	if err := (&payment.ConfirmEngine{Repo: repo,
		Cache: payment.CacheResetFunc(func(ctx context.Context, uid int64) error {
			return rds.Del(ctx, fmt.Sprintf("balance:%d", uid)).Err()
		})}).Register(); err != nil {
		return fmt.Errorf("启动失败: 注册支付确认 handler 失败: %w", err)
	}
	// b3-6：计量聚合 stats:aggregate（cron 触发 → 落 usage_daily_stats）
	if err := (&stats.Engine{Repo: repo}).Register(); err != nil {
		return fmt.Errorf("启动失败: 注册计量聚合 handler 失败: %w", err)
	}

	// dev 单进程（--role=all）自动引导（超管 + 内置种子，幂等）；生产用独立 bootstrap 子命令
	if role == "all" {
		bctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := bootstrap.Run(bctx, db, bootstrap.Config{
			AdminEmail:    cfg.Security.AdminEmail,
			AdminPassword: cfg.Security.AdminPassword,
		}, log)
		cancel()
		if err != nil {
			return fmt.Errorf("启动失败: 引导错误: %w", err)
		}
	}

	roles := map[string]bool{}
	for _, r := range []string{"api", "worker", "scheduler"} {
		roles[r] = role == r || role == "all"
	}

	// 失败信号：任一角色启动/运行致命错误即整体退出
	runErr := make(chan error, 3)
	// 角色 goroutine 等待组：确保优雅关闭时 worker drain / scheduler 退出完成，
	// 不被进程退出截断（01 §7.3 优雅关闭语义）。api 不在此列——其关闭顺序需先于等待。
	var roleWG sync.WaitGroup

	var srv *httpserver.Server
	if roles["api"] {
		srv = httpserver.New(cfg.Server.Addr, db, log)
		// b2-5/6：v1 业务路由装配（鉴权 + 网关编排 + Redis 预扣 + 结算投递）
		gw := &gateway.Gateway{Cat: repo, Bal: repo, List: repo, Res: reserve,
			CredMK: cfg.Security.MasterKey, CredMKPrev: cfg.Security.PreviousMasterKey}
		var enq task.TaskEnqueuer // 结算与支付回调共用 enqueuer
		if enq0, err := schedulerEnqueuer(ctx, cfg); err == nil {
			enq = enq0
			gw.Prod = &billing.Producer{Enq: enq0}
		} else {
			log.Warn("结算投递器不可用（broker 未就绪），本次启动不投递计量任务", "error", err)
		}
		srv.MountV1(&httpserver.API{Store: repo, Salt: cfg.Security.APIKeySalt, Gw: gw, Log: log})
		// b3-1：管理端接口（角色鉴权在 /api/v1/admin 组内强制校验；MK 供渠道凭证信封加密，b3-2 起用）
		srv.MountAdmin(&httpserver.Admin{
			Repo: repo, Store: repo, Salt: cfg.Security.APIKeySalt,
			MK: cfg.Security.MasterKey, MKID: cfg.Security.MasterKeyID,
			MKPrev: cfg.Security.PreviousMasterKey, Log: log,
		})
		// b3-3：认证/自助（注册默认开启；config registration_enabled 落库前显式 true）
		// b3-4：充值支付——真实渠道凭据接入时按 config 构造传入 NewService；
		// mock 仅在 dev(--role=all) 显式注册（生产不启用，防无签名 notify 伪造充值）
		paySvc := payment.NewService(log, nil)
		if role == "all" && len(paySvc.List()) == 0 {
			paySvc.RegisterProvider(&payment.MockProvider{})
			log.Warn("dev(--role=all)：显式启用内置模拟支付渠道（演练用，禁止用于生产）")
		}
		srv.MountPortal(&httpserver.Portal{Repo: repo, Salt: cfg.Security.APIKeySalt,
			Log: log, RegistrationEnabled: true,
			Pay: paySvc, PayEnq: enq, OrderExpire: cfg.Payment.OrderExpire})
		go func() {
			if err := srv.Serve(); err != nil {
				runErr <- fmt.Errorf("api 运行失败: %w", err)
			}
		}()
		log.Info("角色已启动", "role", "api")
	}

	if roles["worker"] {
		roleWG.Add(1)
		go func() {
			defer roleWG.Done()
			runWorker(ctx, cfg, log, runErr)
		}()
	}
	if roles["scheduler"] {
		roleWG.Add(1)
		go func() {
			defer roleWG.Done()
			runScheduler(ctx, cfg, log, runErr)
		}()
	}

	var runError error
	select {
	case runError = <-runErr:
		stop()
		log.Error("角色异常退出，触发整体停止", "error", runError)
	case <-ctx.Done():
		log.Info("收到退出信号，优雅关闭中")
	}

	// 1) 先停 API（不再接收新请求，等 http.ErrServerClosed）
	if srv != nil {
		shCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		err := srv.Shutdown(shCtx)
		cancel()
		if err != nil {
			log.Error("http server 优雅关闭失败", "error", err)
		}
	}
	// 2) 再等待 worker/scheduler drain 完成（含各自 drain 上限）
	waitRoles(&roleWG, cfg.Server.ShutdownTimeout, log)
	return runError
}

// waitRoles 等待角色 goroutine 收敛；超时仅告警不阻塞退出。
func waitRoles(wg *sync.WaitGroup, timeout time.Duration, log *slog.Logger) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	select {
	case <-done:
	case <-time.After(timeout):
		log.Warn("角色优雅关闭超时，强制退出", "timeout", timeout)
	}
}

// openDB 建立 GORM 连接并做初连通校验（模型层 DB 池配置来自 config）。
func openDB(ctx context.Context, cfg *config.Config) (*gorm.DB, func(), error) {
	db, err := model.Open(model.Options{
		DSN:             cfg.Database.DSN,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
	if err != nil {
		return nil, nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.WithContext(pingCtx).Raw("SELECT 1").Error; err != nil {
		return nil, nil, fmt.Errorf("启动失败: 数据库不可达: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	return db, func() { _ = sqlDB.Close() }, nil
}

// runWorker worker 角色：声明拓扑；handler 为空时（B1）不消费只留痕。
func runWorker(ctx context.Context, cfg *config.Config, log *slog.Logger, runErr chan<- error) {
	policy := task.RetryPolicy{
		RetryBuckets: cfg.RabbitMQ.RetryBuckets,
		DelayBuckets: cfg.RabbitMQ.DelayBuckets,
	}
	registered := task.Handlers()
	if err := task.EnsureBrokerTopology(ctx, cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange, policy, task.DefaultRegistry); err != nil {
		if len(registered) == 0 {
			// B1 阶段 worker 无 handler、不消费：broker 不在只告警并 idle，
			// 避免 dev 的 --role=all 因 RabbitMQ 未起而连带杀死 API。
			log.Warn("worker：RabbitMQ 不可达且业务 handler 未接线，worker 待机（拓扑缺失无消费影响）", "error", err)
			<-ctx.Done()
			return
		}
		runErr <- fmt.Errorf("worker 拓扑声明失败: %w", err)
		return
	}
	if len(registered) == 0 {
		log.Warn("worker：业务 handler 未接线（B1 阶段），消费者不启动；拓扑已声明")
		<-ctx.Done()
		return
	}
	opts := task.ConsumerOptions{
		URL:      cfg.RabbitMQ.URL,
		Exchange: cfg.RabbitMQ.Exchange,
		Policy:   policy,
		Registry: task.DefaultRegistry,
		Consumers: map[task.QueueName]task.ConsumerConfig{
			task.QueueCritical: toConsumer(cfg.RabbitMQ.Consumers["critical"]),
			task.QueueDefault:  toConsumer(cfg.RabbitMQ.Consumers["default"]),
			task.QueueLow:      toConsumer(cfg.RabbitMQ.Consumers["low"]),
		},
		Handlers:        registered,
		ShutdownTimeout: cfg.RabbitMQ.ShutdownTimeout,
	}
	if err := task.RunConsumers(ctx, opts); err != nil {
		runErr <- fmt.Errorf("worker 运行失败: %w", err)
	}
}

func toConsumer(c config.ConsumerConfig) task.ConsumerConfig {
	return task.ConsumerConfig{
		Enabled:     c.Enabled,
		Concurrency: c.Concurrency,
		Prefetch:    c.Prefetch,
		MaxRetry:    c.MaxRetry,
		Timeout:     c.Timeout,
	}
}

// runBootstrap 独立引导子命令（生产一次性；--role=all 已自动执行，重复运行幂等）。
func runBootstrap() int {
	dsn := os.Getenv("CLOUDFOG_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "bootstrap: 缺少环境变量 CLOUDFOG_DSN")
		return 1
	}
	adminEmail := os.Getenv("CLOUDFOG_ADMIN_EMAIL")
	adminPass := os.Getenv("CLOUDFOG_ADMIN_PASSWORD")
	if adminEmail == "" || adminPass == "" {
		fmt.Fprintln(os.Stderr, "bootstrap: 缺少 CLOUDFOG_ADMIN_EMAIL / CLOUDFOG_ADMIN_PASSWORD")
		return 1
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrate.RequireSchemaVersion(dsn, schemaVersion); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap:", err)
		return 1
	}
	db, closeDB, err := openDB(ctx, &config.Config{Database: config.DatabaseConfig{DSN: dsn}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer closeDB()
	if err := bootstrap.Run(ctx, db, bootstrap.Config{AdminEmail: adminEmail, AdminPassword: adminPass}, log); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap:", err)
		return 1
	}
	return 0
}

func runMigrate(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: cloudfog migrate up | down <N> | status")
		return 1
	}
	dsn := os.Getenv("CLOUDFOG_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "migrate: 缺少环境变量 CLOUDFOG_DSN")
		return 1
	}
	var err error
	switch args[0] {
	case "up":
		err = migrate.Up(dsn)
	case "down":
		if len(args) < 2 {
			err = fmt.Errorf("migrate down 需要指定回滚步数")
			break
		}
		var n int
		if n, err = strconv.Atoi(args[1]); err == nil {
			err = migrate.Down(dsn, n)
		}
	case "status":
		err = migrate.Status(dsn, os.Stdout)
	default:
		err = fmt.Errorf("未知子命令 %q", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, `cloudfog — 云之雾 API 聚合平台

用法:
  cloudfog migrate up          应用全部待执行迁移
  cloudfog migrate down <N>    回滚 N 步（仅本地/测试）
  cloudfog migrate status      查看迁移状态
  cloudfog bootstrap           首次引导（超管 + 内置种子；幂等，--role=all 自动执行）
  cloudfog --role=api           启动 API 角色（/healthz /readyz）
  cloudfog --role=worker        启动 Worker 角色（消费任务）
  cloudfog --role=scheduler     启动 Scheduler 角色（周期任务基座）
  cloudfog --role=all           全角色（本地开发单进程）
  cloudfog --role=api --config=config.yaml   使用 YAML 配置源（10 §3.1）
`)
}
