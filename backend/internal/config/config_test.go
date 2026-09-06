package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setRequiredEnv 设置全部必填环境变量（可被覆盖注入非法值测试校验规则）。
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDFOG_DSN", "postgres://u:p@127.0.0.1:5432/db?sslmode=disable")
	t.Setenv("CLOUDFOG_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("CLOUDFOG_RABBITMQ_URL", "amqp://u:p@127.0.0.1:5672/cloudfog")
	t.Setenv("CLOUDFOG_MASTER_KEY", strings.Repeat("a", 64))
	t.Setenv("CLOUDFOG_API_KEY_SALT", strings.Repeat("s", 32))
}

func Test_Load_RequiredEnv_Passes(t *testing.T) {
	setRequiredEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Database.DSN == "" || cfg.RabbitMQ.URL == "" {
		t.Fatal("env 别名未生效：DSN/RabbitMQ URL 为空")
	}
	if cfg.RabbitMQ.Exchange != "cloudfog.tasks" {
		t.Fatalf("exchange 默认值 = %q, want cloudfog.tasks", cfg.RabbitMQ.Exchange)
	}
	if cfg.RabbitMQ.Consumers["critical"].Concurrency != 50 {
		t.Fatalf("critical 并发默认值 = %d, want 50", cfg.RabbitMQ.Consumers["critical"].Concurrency)
	}
	if cfg.Billing.ReserveTTL.String() != "1h0m0s" {
		t.Fatalf("billing.reserve_ttl 默认 = %s, want 1h（06 §3.3 时序链）", cfg.Billing.ReserveTTL)
	}
}

func Test_Load_MissingRequired_Fails(t *testing.T) {
	t.Setenv("CLOUDFOG_DSN", "")
	t.Setenv("CLOUDFOG_MASTER_KEY", strings.Repeat("a", 64))
	t.Setenv("CLOUDFOG_API_KEY_SALT", strings.Repeat("s", 32))
	_, err := Load("")
	if err == nil {
		t.Fatal("缺少 DSN 时应校验失败")
	}
	if !strings.Contains(err.Error(), "database.dsn") {
		t.Fatalf("错误信息应包含 database.dsn，got: %v", err)
	}
}

func Test_Load_InvalidMasterKey_Fails(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CLOUDFOG_MASTER_KEY", "short")
	_, err := Load("")
	if err == nil || !strings.Contains(err.Error(), "master_key") {
		t.Fatalf("非法主密钥应报错并包含 master_key，got: %v", err)
	}
}

func Test_Load_ConsumerConcurrencyMonotonic_Fails(t *testing.T) {
	setRequiredEnv(t)
	// 在临时 config.yaml 中把 default 并发调高于 critical（01 §7.1 铁律）
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	yaml := `
rabbitmq:
  consumers:
    critical: { enabled: true, concurrency: 10, prefetch: 10, max_retry: 10, timeout: 30s }
    default:  { enabled: true, concurrency: 50, prefetch: 10, max_retry: 5,  timeout: 10s }
    low:      { enabled: true, concurrency: 1,  prefetch: 10, max_retry: 3,  timeout: 60s }
`
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "critical 并发不得低于 default") {
		t.Fatalf("并发倒挂应报错，got: %v", err)
	}
}

func Test_Load_InvalidRetryBucketOrder_Fails(t *testing.T) {
	setRequiredEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	yaml := `
rabbitmq:
  retry_buckets: [5m, 1m, 5s]
`
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "retry_buckets") {
		t.Fatalf("桶乱序应报错并包含 retry_buckets，got: %v", err)
	}
}

func Test_LoadDotEnv_EnvTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "# comment\nCLOUDFOG_TEST_VAR=from_file\nCLOUDFOG_DSN=from_file\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLOUDFOG_DSN", "from_real_env") // 真实环境变量优先
	os.Unsetenv("CLOUDFOG_TEST_VAR")

	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("CLOUDFOG_DSN"); got != "from_real_env" {
		t.Fatalf("真实环境变量应优先，got %q", got)
	}
	if got := os.Getenv("CLOUDFOG_TEST_VAR"); got != "from_file" {
		t.Fatalf(".env 变量应被加载，got %q", got)
	}
}

// 回归测试：Windows 编辑器保存的 .env 带 UTF-8 BOM，首键不得静默失效。
func Test_LoadDotEnv_BOM_Stripped(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "\uFEFFCLOUDFOG_BOM_PROBE=bom_ok\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("CLOUDFOG_BOM_PROBE")
	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("CLOUDFOG_BOM_PROBE"); got != "bom_ok" {
		t.Fatalf("BOM 应被剥离，got %q", got)
	}
}

// 行内注释剥离（godotenv 语义）；含 # 的密码必须用引号包裹。
func Test_LoadDotEnv_InlineComment(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "COMMENT_PROBE=plain_value # this is a note\nHASH_PASS=\"pa#ss\" # quoted keeps hash\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("COMMENT_PROBE")
	os.Unsetenv("HASH_PASS")
	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("COMMENT_PROBE"); got != "plain_value" {
		t.Fatalf("未加引号值的行内注释应剥离，got %q", got)
	}
	if got := os.Getenv("HASH_PASS"); got != "pa#ss" {
		t.Fatalf("引号值应保留 #，got %q", got)
	}
}

// 回归测试：10 §3.2 YAML 示例的 ${VAR} 占位符写法必须可执行。
func Test_Load_ExpandEnvRefs(t *testing.T) {
	setRequiredEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	yaml := `
database:
  dsn: ${CLOUDFOG_DSN}
`
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("${VAR} 插值应生效: %v", err)
	}
	if cfg.Database.DSN == "" || strings.Contains(cfg.Database.DSN, "${") {
		t.Fatalf("DSN 应展开为环境变量值，got %q", cfg.Database.DSN)
	}
}

// 可选字段引用未设置的环境变量 → 展开为空串（previous_master_key/webhook.url 语义）。
func Test_Load_UnsetOptionalRef_Empty(t *testing.T) {
	setRequiredEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	yaml := `
notification:
  webhook:
    enabled: false
    url: ${NOTIFY_WEBHOOK_URL_MISSING}
security:
  previous_master_key: ${CLOUDFOG_MASTER_KEY_OLD}
`
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("可选引用未设置不应报错: %v", err)
	}
	if cfg.Notification.Webhook.URL != "" || cfg.Security.PreviousMasterKey != "" {
		t.Fatalf("可选引用应展开为空串, url=%q old=%q", cfg.Notification.Webhook.URL, cfg.Security.PreviousMasterKey)
	}
}

// 注：必填键（database.dsn 等）绑定 env 且 Viper 优先级 env > config 文件，
// 因此 config.yaml 中为其写 ${VAR} 是无意义的（恒被 env 覆盖）；
// env 拼错场景由 Test_Load_MissingRequired_Fails 覆盖（报 CLOUDFOG_DSN）。

// 黄金文件测试：10 §3.2 的完整配置示例原样可执行，且每个配置段的值与文档一致。
// 文档改配置清单时必须同步本测试（文档即事实源）。
func Test_Load_DocYamlGolden(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_USER", "mailer")
	t.Setenv("SMTP_PASSWORD", "mail-pass")
	t.Setenv("NOTIFY_WEBHOOK_URL", "https://im.example.com/hook")
	t.Setenv("ALIPAY_APP_ID", "alipay-app")
	t.Setenv("WECHAT_MCH_ID", "wx-mch")
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_x")

	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	yaml := `
server:
  addr: ":8080"
  read_timeout: 60s
  write_timeout: 0
  idle_timeout: 180s
  shutdown_timeout: 30s
  body_max_bytes: 8388608

database:
  dsn: ${CLOUDFOG_DSN}
  max_open_conns: 50
  max_idle_conns: 10
  conn_max_lifetime: 30m

redis:
  addr: ${CLOUDFOG_REDIS_ADDR}
  password: ${CLOUDFOG_REDIS_PASSWORD}
  db: 0
  pool_size: 100

security:
  master_key: ${CLOUDFOG_MASTER_KEY}
  master_key_id: "k_2026_08"
  previous_master_key: ${CLOUDFOG_MASTER_KEY_OLD}
  api_key_salt: ${CLOUDFOG_API_KEY_SALT}

gateway:
  first_token_timeout: 60s
  stream_idle_timeout: 120s
  dial_timeout: 5s
  probe_timeout: 10s
  upstream_max_idle_conns: 200
  upstream_idle_conn_timeout: 90s

scheduling:
  candidate_cache_ttl: 30s
  max_switches: 3
  max_same_channel_retries: 2
  same_retry_base_delay: 500ms
  max_request_scoped_delay: 8s
  switch_backoff: 200ms
  sticky_ttl: 1h
  low_score_threshold: 30
  probe_interval: 5m
  probe_recover_success: 2

circuit_breaker:
  failure_threshold: 5
  error_rate_threshold: 0.5
  min_samples: 20
  window: 60s
  open_duration: 60s
  open_duration_max: 10m
  half_open_max_calls: 3
  half_open_success_threshold: 2

rabbitmq:
  url: ${CLOUDFOG_RABBITMQ_URL}
  exchange: "cloudfog.tasks"
  consumers:
    critical: { enabled: true, concurrency: 50, prefetch: 10, max_retry: 10, timeout: 30s }
    default:  { enabled: true, concurrency: 30, prefetch: 10, max_retry: 5,  timeout: 10s }
    low:      { enabled: true, concurrency: 10, prefetch: 10, max_retry: 3,  timeout: 60s }
  retry_buckets: [5s, 1m, 5m]
  delay_buckets: [5m, 15m, 30m]
  publisher_confirm: true
  shutdown_timeout: 30s

scheduler:
  stats_aggregate: "*/10 * * * *"
  daily_reconcile:  "0 3 * * *"
  channel_probe:    "*/5 * * * *"
  log_archive:      "0 4 1 * *"
  subscription_expire: "0 2 * * *"
  order_close:      "*/5 * * * *"
  idempotency_cleanup: "0 5 * * *"
  key_expire:       "0 1 * * *"
  quota_reset:      "0 0 * * *"
  balance_notify:   "0 9 * * *"
  reserve_reclaim:  "*/15 * * * *"

billing:
  reserve_buffer_ratio: 1.5
  min_reserve_output_tokens: 256
  default_currency: "USD"
  max_cost_per_request: 10.0
  reserve_ttl: 60m
  balance_notify_threshold: 10.0

notification:
  email:
    enabled: true
    smtp_host: ${SMTP_HOST}
    smtp_port: 587
    smtp_user: ${SMTP_USER}
    smtp_password: ${SMTP_PASSWORD}
    from: "noreply@cloudfog.example"
  webhook:
    enabled: false
    url: ${NOTIFY_WEBHOOK_URL}

payment:
  providers:
    alipay: { enabled: true,  app_id: "${ALIPAY_APP_ID}" }
    wechat: { enabled: true,  mch_id: "${WECHAT_MCH_ID}" }
    stripe: { enabled: false, secret_key: "${STRIPE_SECRET_KEY}" }
  order_expire: 30m
  notify_rate_limit: 100

observability:
  log_level: info
  log_format: json
  metrics_enabled: true
  trace_enabled: false
  trace_sample_ratio: 0.01

limits:
  global_concurrency: 10000
  per_ip_rpm: 600
  auth_fail_per_min: 20
  auth_fail_lock_minutes: 15
`
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("10 §3.2 文档示例应原样可执行: %v", err)
	}

	type kv struct {
		name, got, want string
	}
	checks := []kv{
		{"server.addr", cfg.Server.Addr, ":8080"},
		{"server.write_timeout", cfg.Server.WriteTimeout.String(), "0s"},
		{"server.shutdown_timeout", cfg.Server.ShutdownTimeout.String(), "30s"},
		{"database.dsn", cfg.Database.DSN, "postgres://u:p@127.0.0.1:5432/db?sslmode=disable"},
		{"database.conn_max_lifetime", cfg.Database.ConnMaxLifetime.String(), "30m0s"},
		{"redis.addr", cfg.Redis.Addr, "127.0.0.1:6379"},
		{"security.master_key_id", cfg.Security.MasterKeyID, "k_2026_08"},
		{"gateway.first_token_timeout", cfg.Gateway.FirstTokenTimeout.String(), "1m0s"},
		{"gateway.probe_timeout", cfg.Gateway.ProbeTimeout.String(), "10s"},
		{"scheduling.same_retry_base_delay", cfg.Scheduling.SameRetryBaseDelay.String(), "500ms"},
		{"scheduling.max_request_scoped_delay", cfg.Scheduling.MaxRequestScopedDelay.String(), "8s"},
		{"scheduling.sticky_ttl", cfg.Scheduling.StickyTTL.String(), "1h0m0s"},
		{"circuit_breaker.window", cfg.CircuitBreaker.Window.String(), "1m0s"},
		{"circuit_breaker.open_duration_max", cfg.CircuitBreaker.OpenDurationMax.String(), "10m0s"},
		{"rabbitmq.exchange", cfg.RabbitMQ.Exchange, "cloudfog.tasks"},
		{"rabbitmq.consumers.critical.timeout", cfg.RabbitMQ.Consumers["critical"].Timeout.String(), "30s"},
		{"rabbitmq.consumers.low.max_retry", fmt.Sprint(cfg.RabbitMQ.Consumers["low"].MaxRetry), "3"},
		{"rabbitmq.retry_buckets[2]", cfg.RabbitMQ.RetryBuckets[2].String(), "5m0s"},
		{"rabbitmq.delay_buckets[0]", cfg.RabbitMQ.DelayBuckets[0].String(), "5m0s"},
		{"scheduler.reserve_reclaim", cfg.Scheduler.ReserveReclaim, "*/15 * * * *"},
		{"scheduler.quota_reset", cfg.Scheduler.QuotaReset, "0 0 * * *"},
		{"billing.reserve_ttl", cfg.Billing.ReserveTTL.String(), "1h0m0s"},
		{"billing.max_cost_per_request", fmt.Sprint(cfg.Billing.MaxCostPerRequest), "10"},
		{"notification.email.smtp_host", cfg.Notification.Email.SMTPHost, "smtp.example.com"},
		{"notification.email.smtp_password", cfg.Notification.Email.SMTPPassword, "mail-pass"},
		{"payment.providers.alipay.app_id", cfg.Payment.Providers["alipay"].AppID, "alipay-app"},
		{"payment.providers.stripe.enabled", fmt.Sprint(cfg.Payment.Providers["stripe"].Enabled), "false"},
		{"payment.order_expire", cfg.Payment.OrderExpire.String(), "30m0s"},
		{"observability.log_level", cfg.Observability.LogLevel, "info"},
		{"limits.global_concurrency", fmt.Sprint(cfg.Limits.GlobalConcurrency), "10000"},
		{"limits.auth_fail_lock_minutes", fmt.Sprint(cfg.Limits.AuthFailLockMinutes), "15"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("文档示例键 %s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// observability env 别名（CLOUDFOG_LOG_LEVEL/FORMAT）必须可覆盖默认值。
func Test_Load_ObservabilityEnvAlias(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CLOUDFOG_LOG_LEVEL", "debug")
	t.Setenv("CLOUDFOG_LOG_FORMAT", "text")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.Observability.LogLevel != "debug" {
		t.Fatalf("log_level = %q, want debug", cfg.Observability.LogLevel)
	}
	if cfg.Observability.LogFormat != "text" {
		t.Fatalf("log_format = %q, want text", cfg.Observability.LogFormat)
	}
}
