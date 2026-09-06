// Package config 承载全量启动配置（唯一清单：docs/10-deployment.md §3.2）。
// 来源优先级：默认值 < config.yaml < 环境变量（< 命令行参数，由 cmd 层覆盖）。
// 铁律（10 §3.1）：敏感配置绝不写入 config.yaml，只经环境变量注入。
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/spf13/viper"
)

// ── 配置结构（键与 10 §3.2 一一对应，禁止增删键而不改文档）──────────────

type Config struct {
	Server         ServerConfig         `mapstructure:"server"`
	Database       DatabaseConfig       `mapstructure:"database"`
	Redis          RedisConfig          `mapstructure:"redis"`
	Security       SecurityConfig       `mapstructure:"security"`
	Gateway        GatewayConfig        `mapstructure:"gateway"`
	Scheduling     SchedulingConfig     `mapstructure:"scheduling"`
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	RabbitMQ       RabbitMQConfig       `mapstructure:"rabbitmq"`
	Scheduler      SchedulerConfig      `mapstructure:"scheduler"`
	Billing        BillingConfig        `mapstructure:"billing"`
	Notification   NotificationConfig   `mapstructure:"notification"`
	Payment        PaymentConfig        `mapstructure:"payment"`
	Observability  ObservabilityConfig  `mapstructure:"observability"`
	Limits         LimitsConfig         `mapstructure:"limits"`
}

type ServerConfig struct {
	Addr            string        `mapstructure:"addr"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"` // 0 = 流式响应不设写超时
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	BodyMaxBytes    int64         `mapstructure:"body_max_bytes"`
}

type DatabaseConfig struct {
	DSN             string        `mapstructure:"dsn"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

type SecurityConfig struct {
	MasterKey         string `mapstructure:"master_key"` // hex 32 字节（64 字符）
	MasterKeyID       string `mapstructure:"master_key_id"`
	PreviousMasterKey string `mapstructure:"previous_master_key"` // 轮换期使用，可空
	APIKeySalt        string `mapstructure:"api_key_salt"`
	// 引导相关（10 §3.4）：未设置时 bootstrap 使用默认值/随机生成
	AdminEmail    string `mapstructure:"admin_email"`
	AdminPassword string `mapstructure:"admin_password"`
}

type GatewayConfig struct {
	FirstTokenTimeout       time.Duration `mapstructure:"first_token_timeout"`
	StreamIdleTimeout       time.Duration `mapstructure:"stream_idle_timeout"`
	DialTimeout             time.Duration `mapstructure:"dial_timeout"`
	ProbeTimeout            time.Duration `mapstructure:"probe_timeout"`
	UpstreamMaxIdleConns    int           `mapstructure:"upstream_max_idle_conns"`
	UpstreamIdleConnTimeout time.Duration `mapstructure:"upstream_idle_conn_timeout"`
}

type SchedulingConfig struct {
	CandidateCacheTTL     time.Duration `mapstructure:"candidate_cache_ttl"`
	MaxSwitches           int           `mapstructure:"max_switches"`
	MaxSameChannelRetries int           `mapstructure:"max_same_channel_retries"`
	SameRetryBaseDelay    time.Duration `mapstructure:"same_retry_base_delay"`
	MaxRequestScopedDelay time.Duration `mapstructure:"max_request_scoped_delay"`
	SwitchBackoff         time.Duration `mapstructure:"switch_backoff"`
	StickyTTL             time.Duration `mapstructure:"sticky_ttl"`
	LowScoreThreshold     int           `mapstructure:"low_score_threshold"`
	ProbeInterval         time.Duration `mapstructure:"probe_interval"`
	ProbeRecoverSuccess   int           `mapstructure:"probe_recover_success"`
}

type CircuitBreakerConfig struct {
	FailureThreshold         int           `mapstructure:"failure_threshold"`
	ErrorRateThreshold       float64       `mapstructure:"error_rate_threshold"`
	MinSamples               int           `mapstructure:"min_samples"`
	Window                   time.Duration `mapstructure:"window"`
	OpenDuration             time.Duration `mapstructure:"open_duration"`
	OpenDurationMax          time.Duration `mapstructure:"open_duration_max"`
	HalfOpenMaxCalls         int           `mapstructure:"half_open_max_calls"`
	HalfOpenSuccessThreshold int           `mapstructure:"half_open_success_threshold"`
}

// ConsumerConfig 单个业务队列的消费者组配置。
// Enabled=false 不启动该消费者组（任务仍可投递，堆积待启用）；
// MVP 阶段 low 置 false（11 §3.2）。
type ConsumerConfig struct {
	Enabled     bool          `mapstructure:"enabled"`
	Concurrency int           `mapstructure:"concurrency"`
	Prefetch    int           `mapstructure:"prefetch"`
	MaxRetry    int           `mapstructure:"max_retry"`
	Timeout     time.Duration `mapstructure:"timeout"`
}

type RabbitMQConfig struct {
	URL              string                    `mapstructure:"url"`
	Exchange         string                    `mapstructure:"exchange"`
	Consumers        map[string]ConsumerConfig `mapstructure:"consumers"` // key: critical/default/low
	RetryBuckets     []time.Duration           `mapstructure:"retry_buckets"`
	DelayBuckets     []time.Duration           `mapstructure:"delay_buckets"`
	PublisherConfirm bool                      `mapstructure:"publisher_confirm"`
	ShutdownTimeout  time.Duration             `mapstructure:"shutdown_timeout"`
}

// SchedulerConfig 周期任务 cron 表，与 01 §7.2 任务清单一一对应。
type SchedulerConfig struct {
	StatsAggregate     string `mapstructure:"stats_aggregate"`
	DailyReconcile     string `mapstructure:"daily_reconcile"`
	ChannelProbe       string `mapstructure:"channel_probe"`
	LogArchive         string `mapstructure:"log_archive"`
	SubscriptionExpire string `mapstructure:"subscription_expire"`
	OrderClose         string `mapstructure:"order_close"`
	IdempotencyCleanup string `mapstructure:"idempotency_cleanup"`
	KeyExpire          string `mapstructure:"key_expire"`
	QuotaReset         string `mapstructure:"quota_reset"`
	BalanceNotify      string `mapstructure:"balance_notify"`
	ReserveReclaim     string `mapstructure:"reserve_reclaim"`
}

type BillingConfig struct {
	ReserveBufferRatio     float64       `mapstructure:"reserve_buffer_ratio"`
	MinReserveOutputTokens int           `mapstructure:"min_reserve_output_tokens"`
	DefaultCurrency        string        `mapstructure:"default_currency"`
	MaxCostPerRequest      float64       `mapstructure:"max_cost_per_request"`
	ReserveTTL             time.Duration `mapstructure:"reserve_ttl"`
	BalanceNotifyThreshold float64       `mapstructure:"balance_notify_threshold"`
}

type NotificationConfig struct {
	Email   EmailConfig   `mapstructure:"email"`
	Webhook WebhookConfig `mapstructure:"webhook"`
}

type EmailConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	SMTPHost     string `mapstructure:"smtp_host"`
	SMTPPort     int    `mapstructure:"smtp_port"`
	SMTPUser     string `mapstructure:"smtp_user"`
	SMTPPassword string `mapstructure:"smtp_password"`
	From         string `mapstructure:"from"`
}

type WebhookConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	URL     string `mapstructure:"url"`
}

type PaymentProviderConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	AppID     string `mapstructure:"app_id"`
	MchID     string `mapstructure:"mch_id"`
	SecretKey string `mapstructure:"secret_key"`
}

type PaymentConfig struct {
	Providers       map[string]PaymentProviderConfig `mapstructure:"providers"`
	OrderExpire     time.Duration                    `mapstructure:"order_expire"`
	NotifyRateLimit int                              `mapstructure:"notify_rate_limit"`
}

type ObservabilityConfig struct {
	LogLevel         string  `mapstructure:"log_level"`
	LogFormat        string  `mapstructure:"log_format"`
	MetricsEnabled   bool    `mapstructure:"metrics_enabled"`
	TraceEnabled     bool    `mapstructure:"trace_enabled"`
	TraceSampleRatio float64 `mapstructure:"trace_sample_ratio"`
}

type LimitsConfig struct {
	GlobalConcurrency   int `mapstructure:"global_concurrency"`
	PerIPRPM            int `mapstructure:"per_ip_rpm"`
	AuthFailPerMin      int `mapstructure:"auth_fail_per_min"`
	AuthFailLockMinutes int `mapstructure:"auth_fail_lock_minutes"`
}

// ── 加载与校验 ─────────────────────────────────────────────

// flatAliases 扁平环境变量别名（10 §1.3/§3.2 约定的 CLOUDFOG_* 与第三方约定名）。
// Viper 的 AutomaticEnv 只按嵌套键推导 env 名（database.dsn → DATABASE_DSN），
// 文档约定的扁平名必须显式 BindEnv。
var flatAliases = map[string][]string{
	"database.dsn":                        {"CLOUDFOG_DSN"},
	"redis.addr":                          {"CLOUDFOG_REDIS_ADDR"},
	"redis.password":                      {"CLOUDFOG_REDIS_PASSWORD"},
	"rabbitmq.url":                        {"CLOUDFOG_RABBITMQ_URL"},
	"security.master_key":                 {"CLOUDFOG_MASTER_KEY"},
	"security.previous_master_key":        {"CLOUDFOG_MASTER_KEY_OLD"},
	"security.api_key_salt":               {"CLOUDFOG_API_KEY_SALT"},
	"security.admin_email":                {"CLOUDFOG_ADMIN_EMAIL"},
	"security.admin_password":             {"CLOUDFOG_ADMIN_PASSWORD"},
	"notification.email.smtp_host":        {"SMTP_HOST"},
	"notification.email.smtp_user":        {"SMTP_USER"},
	"notification.email.smtp_password":    {"SMTP_PASSWORD"},
	"notification.webhook.url":            {"NOTIFY_WEBHOOK_URL"},
	"observability.log_level":             {"CLOUDFOG_LOG_LEVEL"},
	"observability.log_format":            {"CLOUDFOG_LOG_FORMAT"},
	"payment.providers.alipay.app_id":     {"ALIPAY_APP_ID"},
	"payment.providers.wechat.mch_id":     {"WECHAT_MCH_ID"},
	"payment.providers.stripe.secret_key": {"STRIPE_SECRET_KEY"},
}

// Load 加载配置。configPath 可为空（仅默认值 + 环境变量）。
func Load(configPath string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: 读取配置文件 %s: %w", configPath, err)
		}
	}
	v.AutomaticEnv()
	for key, names := range flatAliases {
		if err := v.BindEnv(append([]string{key}, names...)...); err != nil {
			return nil, fmt.Errorf("config: 绑定环境变量 %s: %w", key, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: 解析配置: %w", err)
	}
	// 10 §3.2 的 YAML 示例使用 ${VAR} 占位符写法——Viper 不做插值，
	// 必须在此展开，否则字面量 "${CLOUDFOG_DSN}" 会通过校验、延迟到连接时才报错。
	if err := cfg.expandEnvRefs(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: 校验失败: %w", err)
	}
	return &cfg, nil
}

// expandEnvRefs 将字符串配置字段中的 ${VAR} 展开为环境变量值。
// VAR 未设置时展开为空串；展开后仍含 "${" 的字段视为拼写错误并报错。
func (c *Config) expandEnvRefs() error {
	refs := map[string]*string{
		"database.dsn":                     &c.Database.DSN,
		"redis.addr":                       &c.Redis.Addr,
		"redis.password":                   &c.Redis.Password,
		"rabbitmq.url":                     &c.RabbitMQ.URL,
		"security.master_key":              &c.Security.MasterKey,
		"security.previous_master_key":     &c.Security.PreviousMasterKey,
		"security.api_key_salt":            &c.Security.APIKeySalt,
		"security.admin_email":             &c.Security.AdminEmail,
		"security.admin_password":          &c.Security.AdminPassword,
		"notification.email.smtp_host":     &c.Notification.Email.SMTPHost,
		"notification.email.smtp_user":     &c.Notification.Email.SMTPUser,
		"notification.email.smtp_password": &c.Notification.Email.SMTPPassword,
		"notification.webhook.url":         &c.Notification.Webhook.URL,
	}
	for _, p := range refs {
		*p = expandString(*p)
	}
	// map 值不可寻址，单独处理支付渠道密钥
	for code, p := range c.Payment.Providers {
		p.AppID = expandString(p.AppID)
		p.MchID = expandString(p.MchID)
		p.SecretKey = expandString(p.SecretKey)
		c.Payment.Providers[code] = p
	}
	return nil
}

var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandString 展开 ${VAR}；VAR 未设置 → 空串。
// 语义说明：previous_master_key、webhook.url 等为可选项（10 §3.2），
// 引用未设置的环境变量属正常，不得在此报错；必填字段的缺失由 Validate
// 的"不能为空"规则给出清晰报错（含环境变量提示）。
func expandString(s string) string {
	return envRefPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return ""
	})
}

func setDefaults(v *viper.Viper) {
	// server
	v.SetDefault("server.addr", ":8080")
	v.SetDefault("server.read_timeout", "60s")
	v.SetDefault("server.write_timeout", 0) // 流式响应不设写超时
	v.SetDefault("server.idle_timeout", "180s")
	v.SetDefault("server.shutdown_timeout", "30s")
	v.SetDefault("server.body_max_bytes", 8388608)
	// database
	v.SetDefault("database.dsn", "")
	v.SetDefault("database.max_open_conns", 50)
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.conn_max_lifetime", "30m")
	// redis
	v.SetDefault("redis.addr", "")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 100)
	// security
	v.SetDefault("security.master_key", "")
	v.SetDefault("security.master_key_id", "k_2026_08")
	v.SetDefault("security.previous_master_key", "")
	v.SetDefault("security.api_key_salt", "")
	v.SetDefault("security.admin_email", "")
	v.SetDefault("security.admin_password", "")
	// gateway
	v.SetDefault("gateway.first_token_timeout", "60s")
	v.SetDefault("gateway.stream_idle_timeout", "120s")
	v.SetDefault("gateway.dial_timeout", "5s")
	v.SetDefault("gateway.probe_timeout", "10s")
	v.SetDefault("gateway.upstream_max_idle_conns", 200)
	v.SetDefault("gateway.upstream_idle_conn_timeout", "90s")
	// scheduling
	v.SetDefault("scheduling.candidate_cache_ttl", "30s")
	v.SetDefault("scheduling.max_switches", 3)
	v.SetDefault("scheduling.max_same_channel_retries", 2)
	v.SetDefault("scheduling.same_retry_base_delay", "500ms")
	v.SetDefault("scheduling.max_request_scoped_delay", "8s")
	v.SetDefault("scheduling.switch_backoff", "200ms")
	v.SetDefault("scheduling.sticky_ttl", "1h")
	v.SetDefault("scheduling.low_score_threshold", 30)
	v.SetDefault("scheduling.probe_interval", "5m")
	v.SetDefault("scheduling.probe_recover_success", 2)
	// circuit_breaker（05 §7 默认值）
	v.SetDefault("circuit_breaker.failure_threshold", 5)
	v.SetDefault("circuit_breaker.error_rate_threshold", 0.5)
	v.SetDefault("circuit_breaker.min_samples", 20)
	v.SetDefault("circuit_breaker.window", "60s")
	v.SetDefault("circuit_breaker.open_duration", "60s")
	v.SetDefault("circuit_breaker.open_duration_max", "10m")
	v.SetDefault("circuit_breaker.half_open_max_calls", 3)
	v.SetDefault("circuit_breaker.half_open_success_threshold", 2)
	// rabbitmq（01 §7.1 / 10 §3.2）
	v.SetDefault("rabbitmq.url", "")
	v.SetDefault("rabbitmq.exchange", "cloudfog.tasks")
	v.SetDefault("rabbitmq.consumers.critical.enabled", true)
	v.SetDefault("rabbitmq.consumers.critical.concurrency", 50)
	v.SetDefault("rabbitmq.consumers.critical.prefetch", 10)
	v.SetDefault("rabbitmq.consumers.critical.max_retry", 10)
	v.SetDefault("rabbitmq.consumers.critical.timeout", "30s")
	v.SetDefault("rabbitmq.consumers.default.enabled", true)
	v.SetDefault("rabbitmq.consumers.default.concurrency", 30)
	v.SetDefault("rabbitmq.consumers.default.prefetch", 10)
	v.SetDefault("rabbitmq.consumers.default.max_retry", 5)
	v.SetDefault("rabbitmq.consumers.default.timeout", "10s")
	v.SetDefault("rabbitmq.consumers.low.enabled", true)
	v.SetDefault("rabbitmq.consumers.low.concurrency", 10)
	v.SetDefault("rabbitmq.consumers.low.prefetch", 10)
	v.SetDefault("rabbitmq.consumers.low.max_retry", 3)
	v.SetDefault("rabbitmq.consumers.low.timeout", "60s")
	v.SetDefault("rabbitmq.retry_buckets", []string{"5s", "1m", "5m"})
	v.SetDefault("rabbitmq.delay_buckets", []string{"5m", "15m", "30m"})
	v.SetDefault("rabbitmq.publisher_confirm", true)
	v.SetDefault("rabbitmq.shutdown_timeout", "30s")
	// scheduler（cron 与 01 §7.2 一一对应）
	v.SetDefault("scheduler.stats_aggregate", "*/10 * * * *")
	v.SetDefault("scheduler.daily_reconcile", "0 3 * * *")
	v.SetDefault("scheduler.channel_probe", "*/5 * * * *")
	v.SetDefault("scheduler.log_archive", "0 4 1 * *")
	v.SetDefault("scheduler.subscription_expire", "0 2 * * *")
	v.SetDefault("scheduler.order_close", "*/5 * * * *")
	v.SetDefault("scheduler.idempotency_cleanup", "0 5 * * *")
	v.SetDefault("scheduler.key_expire", "0 1 * * *")
	v.SetDefault("scheduler.quota_reset", "0 0 * * *")
	v.SetDefault("scheduler.balance_notify", "0 9 * * *")
	v.SetDefault("scheduler.reserve_reclaim", "*/15 * * * *")
	// billing
	v.SetDefault("billing.reserve_buffer_ratio", 1.5)
	v.SetDefault("billing.min_reserve_output_tokens", 256)
	v.SetDefault("billing.default_currency", "USD")
	v.SetDefault("billing.max_cost_per_request", 10.0)
	v.SetDefault("billing.reserve_ttl", "60m")
	v.SetDefault("billing.balance_notify_threshold", 10.0)
	// notification
	// 注意：smtp_host/user/password、webhook.url 必须占位 SetDefault——
	// BindEnv 注册的键不进 AllKeys，无默认值时 Unmarshal 读不到环境变量值。
	v.SetDefault("notification.email.enabled", true)
	v.SetDefault("notification.email.smtp_host", "")
	v.SetDefault("notification.email.smtp_port", 587)
	v.SetDefault("notification.email.smtp_user", "")
	v.SetDefault("notification.email.smtp_password", "")
	v.SetDefault("notification.email.from", "noreply@cloudfog.example")
	v.SetDefault("notification.webhook.enabled", false)
	v.SetDefault("notification.webhook.url", "")
	// payment
	v.SetDefault("payment.providers.alipay.enabled", true)
	v.SetDefault("payment.providers.alipay.app_id", "")
	v.SetDefault("payment.providers.wechat.enabled", true)
	v.SetDefault("payment.providers.wechat.mch_id", "")
	v.SetDefault("payment.providers.stripe.enabled", false)
	v.SetDefault("payment.providers.stripe.secret_key", "")
	v.SetDefault("payment.order_expire", "30m")
	v.SetDefault("payment.notify_rate_limit", 100)
	// observability
	v.SetDefault("observability.log_level", "info")
	v.SetDefault("observability.log_format", "json")
	v.SetDefault("observability.metrics_enabled", true)
	v.SetDefault("observability.trace_enabled", false)
	v.SetDefault("observability.trace_sample_ratio", 0.01)
	// limits
	v.SetDefault("limits.global_concurrency", 10000)
	v.SetDefault("limits.per_ip_rpm", 600)
	v.SetDefault("limits.auth_fail_per_min", 20)
	v.SetDefault("limits.auth_fail_lock_minutes", 15)
}

// Validate 启动校验（01 §10：必填项缺失则启动失败并明确报错）。
func (c *Config) Validate() error {
	var errs []error
	req := func(cond bool, format string, args ...any) {
		if cond {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	req(c.Database.DSN == "", "database.dsn 不能为空（环境变量 CLOUDFOG_DSN）")
	req(c.Redis.Addr == "", "redis.addr 不能为空（环境变量 CLOUDFOG_REDIS_ADDR）")
	req(c.RabbitMQ.URL == "", "rabbitmq.url 不能为空（环境变量 CLOUDFOG_RABBITMQ_URL）")
	req(c.RabbitMQ.URL != "" && !strings.HasPrefix(c.RabbitMQ.URL, "amqp://"),
		"rabbitmq.url 必须以 amqp:// 开头")
	req(c.RabbitMQ.Exchange == "", "rabbitmq.exchange 不能为空")
	req(!isValidMasterKey(c.Security.MasterKey),
		"security.master_key 必须是 64 位 hex（32 字节），环境变量 CLOUDFOG_MASTER_KEY")
	req(c.Security.PreviousMasterKey != "" && !isValidMasterKey(c.Security.PreviousMasterKey),
		"security.previous_master_key 设置时必须是 64 位 hex（32 字节）")
	req(c.Security.PreviousMasterKey != "" && c.Security.PreviousMasterKey == c.Security.MasterKey,
		"security.previous_master_key 不得与当前主密钥相同（轮换期双密钥并存，同值即轮换失效）")
	req(len(c.Security.APIKeySalt) < 16,
		"security.api_key_salt 至少 16 字符（环境变量 CLOUDFOG_API_KEY_SALT）")
	req(c.Server.Addr == "", "server.addr 不能为空")

	// 消费者并发必须随优先级单调递减（01 §7.1 铁律），仅对启用的组生效
	cc, ok1 := c.RabbitMQ.Consumers["critical"]
	cd, ok2 := c.RabbitMQ.Consumers["default"]
	cl, ok3 := c.RabbitMQ.Consumers["low"]
	req(!ok1 || !ok2 || !ok3, "rabbitmq.consumers 必须包含 critical/default/low 三组")
	if ok1 && ok2 && ok3 {
		if cc.Enabled && cd.Enabled && cc.Concurrency < cd.Concurrency {
			errs = append(errs, errors.New("rabbitmq.consumers: critical 并发不得低于 default（结算优先语义）"))
		}
		if cd.Enabled && cl.Enabled && cd.Concurrency < cl.Concurrency {
			errs = append(errs, errors.New("rabbitmq.consumers: default 并发不得低于 low"))
		}
	}
	for name, cc := range c.RabbitMQ.Consumers {
		req(cc.Concurrency <= 0, "rabbitmq.consumers.%s.concurrency 必须为正", name)
		req(cc.Prefetch <= 0, "rabbitmq.consumers.%s.prefetch 必须为正", name)
		req(cc.Enabled && cc.Timeout <= 0, "rabbitmq.consumers.%s.timeout 必须为正", name)
	}
	req(len(c.RabbitMQ.RetryBuckets) == 0, "rabbitmq.retry_buckets 不能为空")
	req(!ascending(c.RabbitMQ.RetryBuckets), "rabbitmq.retry_buckets 必须按时间升序（应用级退避桶）")
	req(len(c.RabbitMQ.DelayBuckets) == 0, "rabbitmq.delay_buckets 不能为空")
	req(!ascending(c.RabbitMQ.DelayBuckets), "rabbitmq.delay_buckets 必须按时间升序（EnqueueIn 取 ≥ 请求延迟的最小档位，06 §7.4.1）")

	req(c.Billing.ReserveTTL <= 0, "billing.reserve_ttl 必须为正")
	req(c.Billing.MaxCostPerRequest <= 0, "billing.max_cost_per_request 必须为正")
	req(c.Limits.GlobalConcurrency <= 0, "limits.global_concurrency 必须为正")
	req(c.Server.ShutdownTimeout <= 0, "server.shutdown_timeout 必须为正")
	req(c.RabbitMQ.ShutdownTimeout <= 0, "rabbitmq.shutdown_timeout 必须为正")

	// observability 枚举 fail-fast：logger 对未知级别会静默回退 info，
	// 拼写错误必须在启动期暴露（与 cron 校验同策略）
	switch strings.ToLower(c.Observability.LogLevel) {
	case "debug", "info", "warn", "warning", "error":
	default:
		errs = append(errs, fmt.Errorf("observability.log_level 非法: %q（debug|info|warn|error）", c.Observability.LogLevel))
	}
	switch strings.ToLower(c.Observability.LogFormat) {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("observability.log_format 非法: %q（json|text）", c.Observability.LogFormat))
	}

	// 正数时长校验（表驱动紧凑落地；0 值语义仅 server.write_timeout 允许）
	positives := map[string]time.Duration{
		"gateway.first_token_timeout":        c.Gateway.FirstTokenTimeout,
		"gateway.stream_idle_timeout":        c.Gateway.StreamIdleTimeout,
		"gateway.dial_timeout":               c.Gateway.DialTimeout,
		"gateway.probe_timeout":              c.Gateway.ProbeTimeout,
		"gateway.upstream_idle_conn_timeout": c.Gateway.UpstreamIdleConnTimeout,
		"scheduling.candidate_cache_ttl":     c.Scheduling.CandidateCacheTTL,
		"scheduling.same_retry_base_delay":   c.Scheduling.SameRetryBaseDelay,
		"scheduling.sticky_ttl":              c.Scheduling.StickyTTL,
		"scheduling.probe_interval":          c.Scheduling.ProbeInterval,
		"circuit_breaker.window":             c.CircuitBreaker.Window,
		"circuit_breaker.open_duration":      c.CircuitBreaker.OpenDuration,
		"circuit_breaker.open_duration_max":  c.CircuitBreaker.OpenDurationMax,
	}
	for name, d := range positives {
		req(d <= 0, "%s 必须为正", name)
	}
	req(c.CircuitBreaker.OpenDurationMax < c.CircuitBreaker.OpenDuration,
		"circuit_breaker.open_duration_max 不得小于 open_duration")
	req(c.CircuitBreaker.ErrorRateThreshold <= 0 || c.CircuitBreaker.ErrorRateThreshold > 1,
		"circuit_breaker.error_rate_threshold 必须在 (0,1] 区间")
	req(c.Observability.TraceSampleRatio < 0 || c.Observability.TraceSampleRatio > 1,
		"observability.trace_sample_ratio 必须在 [0,1] 区间")
	req(c.Billing.ReserveBufferRatio < 1, "billing.reserve_buffer_ratio 不得小于 1（否则自适应预扣不足）")
	req(c.Security.MasterKeyID == "", "security.master_key_id 不能为空（信封加密 cred_key_id 依赖）")

	// 周期任务 cron 表达式启动期校验：robfig/cron 的 AddFunc 对非法表达式会
	// panic（而不是返回错误），必须在此 fail-fast，给出可定位的配置键名。
	// 空字符串 = 不启用该周期任务（MVP 裁剪场景，见 11 §3.2）。
	for name, spec := range map[string]string{
		"stats_aggregate":     c.Scheduler.StatsAggregate,
		"daily_reconcile":     c.Scheduler.DailyReconcile,
		"channel_probe":       c.Scheduler.ChannelProbe,
		"log_archive":         c.Scheduler.LogArchive,
		"subscription_expire": c.Scheduler.SubscriptionExpire,
		"order_close":         c.Scheduler.OrderClose,
		"idempotency_cleanup": c.Scheduler.IdempotencyCleanup,
		"key_expire":          c.Scheduler.KeyExpire,
		"quota_reset":         c.Scheduler.QuotaReset,
		"balance_notify":      c.Scheduler.BalanceNotify,
		"reserve_reclaim":     c.Scheduler.ReserveReclaim,
	} {
		if strings.TrimSpace(spec) == "" {
			continue
		}
		if _, err := cron.ParseStandard(spec); err != nil {
			errs = append(errs, fmt.Errorf("scheduler.%s: 非法 cron 表达式 %q: %w", name, spec, err))
		}
	}

	return errors.Join(errs...)
}

// ascending 校验时长切片严格升序（重试/延迟桶的语义前提）。
// 空切片视为 true——空与非空的区分由调用方的"不能为空"检查负责，避免错误信息误导。
func ascending(ts []time.Duration) bool {
	for i := 1; i < len(ts); i++ {
		if ts[i] <= ts[i-1] {
			return false
		}
	}
	return true
}

func isValidMasterKey(hex string) bool {
	if len(hex) != 64 {
		return false
	}
	for _, r := range strings.ToLower(hex) {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
