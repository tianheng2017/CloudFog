# 10 · 部署与运维

> 本文描述部署拓扑、容器编排、配置项、扩缩容、备份恢复与发布流程。
> 阅读本文后你应当能回答：怎么把系统跑起来、配置怎么管、出问题怎么回滚。

---

## 1. 部署拓扑

### 1.1 小规模（MVP，单机）

```mermaid
flowchart TB
    subgraph Host["单台服务器（4C8G 起步）"]
        C1["cloudfog --role=all<br/>API + Worker + Scheduler"]
        PG[("PostgreSQL 18")]
        RD[("Redis 8")]
        NG["Caddy / Nginx<br/>TLS 终结 + 反向代理"]
    end
    Internet --> NG --> C1
    C1 --> PG
    C1 --> RD
```

适合：日调用 < 10 万，团队自用或内测。

### 1.2 生产（推荐）

```mermaid
flowchart TB
    LB["负载均衡<br/>（云 LB / Nginx）"] --> FE["frontend<br/>（Nuxt 4 SSR × N）"]
    FE --> API1["cloudfog --role=api #1"]
    FE --> API2["cloudfog --role=api #2"]
    FE --> API3["cloudfog --role=api #N"]

    RMQ[("RabbitMQ<br/>任务队列")]
    RD[("Redis 主从 + 哨兵<br/>缓存 · 限流 · 锁")]
    API1 -.publish.-> RMQ
    API2 -.publish.-> RMQ
    API3 -.publish.-> RMQ
    API1 --> RD
    API2 --> RD
    API3 --> RD
    API1 --> PG[("PostgreSQL 主")]
    API2 --> PG
    API3 --> PG

    W1["cloudfog --role=worker #1"] -.consume.-> RMQ
    W2["cloudfog --role=worker #2"] -.consume.-> RMQ
    W1 --> RD
    W2 --> RD
    W1 --> PG
    W2 --> PG

    SC["cloudfog --role=scheduler<br/>单实例 · 进程内 cron"] -.publish.-> RMQ
    SC --> RD

    PG --> PGR[("PostgreSQL 只读副本")]
    PGR --> OPS["运维查询 / 报表"]

    MON["Prometheus + Grafana<br/>RabbitMQ Management"]
    API1 --> MON
    W1 --> MON
    RMQ --> MON
```

| 组件 | 副本数 | 扩缩容依据 |
| --- | --- | --- |
| API 节点 | ≥2 | CPU 使用率、在途请求数、P95 延迟 |
| Frontend（Nuxt SSR） | ≥2 | CPU、TTFB（公开页 < 200ms，`swr` 缓存兜底） |
| Worker 节点 | ≥1 | 队列 ready 深度（`rabbitmq_queue_messages_ready`）、结算延迟 |
| Scheduler | **1**（不可多实例） | 固定；挂掉后重启即可（任务注册幂等） |
| PostgreSQL | 主 + 只读副本 | 磁盘、连接数、慢查询 |
| RabbitMQ | MVP 单节点 → 规模化 3 节点 quorum 集群 | 队列深度（ready/unacked）、磁盘剩余、内存水位、publish/consume 速率 |
| Redis | 主从 + 哨兵（或集群） | 内存使用率（< 70%） |

### 1.3 本地开发环境接入（Windows）

> 本项目开发环境存储运行在 **Docker**（PostgreSQL 18 + Redis 8.10 + RabbitMQ 4.2），由 `deploy/docker-compose.dev.yml` 编排（2026-09-06 起；原"复用本机 PG 实例"方案因本机无 PG 服务而废弃）。这是 B1 验收（`make dev`、`/healthz`、建表）的直接前置。**本地开发同样全容器化**，与测试、生产使用相同镜像版本，杜绝环境漂移。

| 项 | 值 | 说明 |
| --- | --- | --- |
| PostgreSQL | `127.0.0.1:5432` | 容器 `cloudfog-postgres`（`postgres:18-alpine`），用户/密码/库均为 `cloudfog`；数据持久化于卷 `cloudfog-dev_pgdata` |
| Redis | `127.0.0.1:6379` | 容器 `cloudfog-redis`（`redis:8.10-alpine`），无密码，AOF 开启，`noeviction` |
| RabbitMQ | AMQP `127.0.0.1:5672`；管理 UI `127.0.0.1:15672` | 容器 `cloudfog-rabbitmq`（`rabbitmq:4.2-management`），用户/密码/vhost 均为 `cloudfog`；数据持久化于卷 `cloudfog-dev_rabbitmqdata` |
| 编排 | `deploy/docker-compose.dev.yml` | `docker compose -f deploy/docker-compose.dev.yml up -d`；健康检查就绪后再启动应用 |

**初始化步骤（PowerShell）**：

```powershell
# 1. 拉起存储（容器不存在时自动创建；已存在且健康则跳过）
docker compose -f deploy/docker-compose.dev.yml up -d
docker ps --filter "name=cloudfog"   # 等待两个容器 (healthy)

# 2. 项目根目录创建 .env（已加入 .gitignore，模板如下，与 .env.example 一致）
CLOUDFOG_DSN=postgres://cloudfog:cloudfog@127.0.0.1:5432/cloudfog?sslmode=disable
CLOUDFOG_REDIS_ADDR=127.0.0.1:6379
CLOUDFOG_REDIS_PASSWORD=
CLOUDFOG_RABBITMQ_URL=amqp://cloudfog:cloudfog@127.0.0.1:5672/cloudfog
RABBITMQ_USER=cloudfog
RABBITMQ_PASSWORD=cloudfog              # §2 compose 的 rabbitmq 服务引用；生产必须改强密码；仅空卷首次初始化生效，之后改密须 rabbitmqctl
CLOUDFOG_MASTER_KEY=<openssl rand -hex 32 生成的 64 位 hex>
CLOUDFOG_API_KEY_SALT=<随机 32+ 字符>
CLOUDFOG_ADMIN_EMAIL=admin@cloudfog.local
CLOUDFOG_ADMIN_PASSWORD=<首次登录后立即修改>

# 3. 应用迁移并启动（引导会自动创建超管与内置种子数据）
cloudfog migrate up     # 或 go run ./cmd/cloudfog migrate up
go run ./cmd/cloudfog --role=all
curl http://127.0.0.1:8080/healthz    # 期望 200；PG 中应看到 20+ 张表
```

**隔离保证**：本地容器独立于其他项目（`project-*` 的 redis 集群与 kafka），凭据与卷均以 `cloudfog` 前缀命名，互不影响。

---

## 2. Docker Compose 编排

```yaml
# docker-compose.yml（生产精简示例）
services:
  postgres:
    image: postgres:18-alpine
    environment:
      POSTGRES_DB: cloudfog
      POSTGRES_USER: cloudfog
      POSTGRES_PASSWORD: ${PG_PASSWORD}
    volumes:
      # PG18+ 镜像数据目录约定：挂 /var/lib/postgresql（数据落在 18/docker 子目录，
      # 支持 pg_upgrade --link）；挂旧路径 .../data 会因 "unused mount/volume" 拒绝启动
      - pgdata:/var/lib/postgresql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U cloudfog"]
      interval: 10s
      retries: 5
    restart: unless-stopped

  redis:
    image: redis:8.10-alpine
    command: redis-server --appendonly yes --maxmemory 2gb --maxmemory-policy noeviction
    volumes:
      - redisdata:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      retries: 5
    restart: unless-stopped

  # 一次性迁移任务：所有应用服务等待其成功结束（service_completed_successfully）
  migrate:
    image: cloudfog:latest
    command: ["migrate", "up"]
    env_file: .env
    depends_on:
      postgres: { condition: service_healthy }
    restart: "no"

  api:
    image: cloudfog:latest
    command: ["--role=api"]
    env_file: .env
    depends_on:
      postgres: { condition: service_healthy }
      redis:    { condition: service_healthy }
      rabbitmq: { condition: service_healthy }
      migrate:  { condition: service_completed_successfully }
    ports: ["8080:8080"]
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/healthz"]
      interval: 15s
      retries: 5
    deploy:
      replicas: 2
    restart: unless-stopped

  worker:
    image: cloudfog:latest
    command: ["--role=worker"]
    env_file: .env
    depends_on:
      postgres: { condition: service_healthy }
      redis:    { condition: service_healthy }
      rabbitmq: { condition: service_healthy }
      migrate:  { condition: service_completed_successfully }
    deploy:
      replicas: 2
    restart: unless-stopped

  scheduler:
    image: cloudfog:latest
    command: ["--role=scheduler"]
    env_file: .env
    depends_on:
      redis:   { condition: service_healthy }
      rabbitmq: { condition: service_healthy }
      migrate: { condition: service_completed_successfully }
    restart: unless-stopped

  # Nuxt 4 SSR 常驻进程（14 §1.4）；公开页 SEO 依赖服务端渲染
  frontend:
    build:
      context: ..
      dockerfile: frontend/Dockerfile
    environment:
      NITRO_PORT: 3000
      CLOUDFOG_API_BASE_URL: http://api:8080   # SSR 服务端取数走内网
    depends_on:
      api: { condition: service_healthy }
    ports:
      - "3000:3000"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:3000/"]
      interval: 15s
      retries: 5
    restart: unless-stopped

  rabbitmq:
    image: rabbitmq:4.2-management
    environment:
      RABBITMQ_DEFAULT_USER: ${RABBITMQ_USER}
      RABBITMQ_DEFAULT_PASS: ${RABBITMQ_PASSWORD}
      RABBITMQ_DEFAULT_VHOST: cloudfog
    volumes:
      - rabbitmqdata:/var/lib/rabbitmq
      # 显式启用插件（-management 变体已默认含 management，此处一并启用 Prometheus 指标）：
      #   [rabbitmq_management,rabbitmq_prometheus].
      - ./rabbitmq/enabled_plugins:/etc/rabbitmq/enabled_plugins:ro
    ports:
      - "5672:5672"     # AMQP 0-9-1（应用连接）
      - "15672:15672"   # Management UI（HTTP API 同端口）
      - "15692:15692"   # Prometheus 指标
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "-q", "ping"]
      interval: 15s
      timeout: 10s
      retries: 10
      start_period: 30s
    restart: unless-stopped

volumes:
  pgdata:
  redisdata:
  rabbitmqdata:
```

> **Redis 内存策略必须是 `noeviction`**：限流计数与分布式锁不允许被 LRU 淘汰（队列任务已迁出至 RabbitMQ，不受影响）。
> **RabbitMQ 持久化卷 `/var/lib/rabbitmq` 必须挂载**：durable 队列的元数据与持久化消息都存于此，丢失即丢任务。`RABBITMQ_DEFAULT_*` 环境变量**仅在空卷首次初始化时生效**，之后修改须走 `rabbitmqctl` 或 Management API——这是该镜像最常见的兼容性陷阱。

**启动顺序**：PG/RabbitMQ/Redis 健康 → `migrate` 容器执行迁移（一次性）→ 启动 Scheduler → 启动 Worker → 启动 API（由 `service_completed_successfully` 依赖条件自动保证）。

### 2.1 镜像构建（Dockerfile）

> **全平台统一容器交付**：本地开发（§1.3）、集成测试（testcontainers）、生产（Compose / Kubernetes）使用**同一镜像与同一依赖版本**，不再提供裸机 / systemd 部署路径。镜像 tag 用 `cloudfog:<git-sha>`（可追溯）+ `cloudfog:latest`（仅最新），由 CI 构建并推送镜像仓库。

```dockerfile
# backend/Dockerfile（多阶段构建，与 Go 1.27.1 基线锁定一致）
FROM golang:1.27.1-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cloudfog ./cmd/cloudfog

FROM alpine:3.24
# ca-certificates：回源上游 HTTPS；tzdata：IANA 时区（02 §13.4 禁止固定偏移量）
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 cloudfog
USER cloudfog
COPY --from=builder /out/cloudfog /usr/local/bin/cloudfog
EXPOSE 8080
ENTRYPOINT ["cloudfog"]
```

| 构建要点 | 说明 |
| --- | --- |
| `CGO_ENABLED=0` | pgx 驱动纯 Go 实现，静态编译无 libc 依赖，镜像体积小、可跑 scratch/alpine |
| 非 root 运行 | `USER 10001`，容器逃逸风险最小化（[08 §8](./08-security.md#8-审计日志) 安全基线） |
| `tzdata` | 时区数据库必须打进镜像，否则 `time.LoadLocation` 失败（02 §13.4） |
| 单镜像多角色 | API/Worker/Scheduler 共用镜像，由 `--role` 参数切换（01 §2），符合"单二进制多职责" |
| 版本连锁 | Go 升级时需同步：`go.mod`、CI `go-version-file`、本 Dockerfile 构建镜像 tag（三处一致） |

---

## 3. 配置管理

### 3.1 配置来源与优先级

```
默认值 < config.yaml < 环境变量 < 命令行参数
```

| 类型 | 存放位置 | 示例 |
| --- | --- | --- |
| 非敏感配置 | `config.yaml`（可入库，随代码版本管理） | 端口、超时、队列并发、调度参数 |
| 敏感配置 | **仅环境变量 / 密钥管理服务** | 数据库密码、Redis 密码、主加密密钥、支付密钥 |
| 运行时可调项 | `settings` 表（管理端可改，热更新） | 注册开关、默认分组、通知模板、风控阈值 |

**铁律**：敏感配置**绝不**写入 `config.yaml` 或代码仓库。CI 中用 gitleaks 扫描。

### 3.2 配置项清单（按模块）

```yaml
server:
  addr: ":8080"
  read_timeout: 60s
  write_timeout: 0        # 流式响应不设写超时
  idle_timeout: 180s
  shutdown_timeout: 30s   # 优雅关闭等待上限
  body_max_bytes: 8388608

database:
  dsn: ${CLOUDFOG_DSN}                 # 来自环境变量
  max_open_conns: 50
  max_idle_conns: 10
  conn_max_lifetime: 30m

redis:
  addr: ${CLOUDFOG_REDIS_ADDR}
  password: ${CLOUDFOG_REDIS_PASSWORD}
  db: 0
  pool_size: 100

security:
  master_key: ${CLOUDFOG_MASTER_KEY}   # 主加密密钥（hex 32 字节）
  master_key_id: "k_2026_08"
  previous_master_key: ${CLOUDFOG_MASTER_KEY_OLD}   # 轮换期使用
  api_key_salt: ${CLOUDFOG_API_KEY_SALT}

gateway:
  first_token_timeout: 60s
  stream_idle_timeout: 120s
  dial_timeout: 5s
  probe_timeout: 10s                # 主动健康检查超时
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
  url: ${CLOUDFOG_RABBITMQ_URL}       # amqp://user:pass@host:5672/cloudfog
  exchange: "cloudfog.tasks"          # direct · durable（拓扑声明见 01 §7.1）
  # 消费者并发必须满足 critical >= default > low，否则"优先级"形同虚设
  # （早期版本 critical=20 / default=50，default 反超 critical，与 01 §7.1 的语义冲突）
  consumers:
    critical: { enabled: true, concurrency: 50, prefetch: 10, max_retry: 10, timeout: 30s }
    default:  { enabled: true, concurrency: 30, prefetch: 10, max_retry: 5,  timeout: 10s }
    low:      { enabled: true, concurrency: 10, prefetch: 10, max_retry: 3,  timeout: 60s }
  # enabled: false 时不启动该队列的消费者组（任务仍可投递，堆积待启用）；
  # MVP 阶段 low 置 enabled: false（见 11 §3.2）。
  retry_buckets: [5s, 1m, 5m]         # 应用级重试 TTL 桶（指数退避近似，上限 5m，见 06 §4.1）
  delay_buckets: [5m, 15m, 30m]       # EnqueueIn 延迟档位（TTL+DLX 队列，见 06 §7.4.1）
  publisher_confirm: true             # 发布确认 + mandatory，投递失败立即报错告警
  shutdown_timeout: 30s
  # broker 侧 consumer_timeout 默认 30min，远大于任务超时上限，无需调整

# 周期任务清单与 [01 §7.2](./01-architecture.md#72-任务清单) 一一对应
scheduler:
  stats_aggregate: "*/10 * * * *"     # 每 10 分钟
  daily_reconcile:  "0 3 * * *"       # 每日 3 点（平台时区）
  channel_probe:    "*/5 * * * *"     # 每 5 分钟
  log_archive:      "0 4 1 * *"       # 每月 1 号 4 点
  subscription_expire: "0 2 * * *"
  order_close:      "*/5 * * * *"
  idempotency_cleanup: "0 5 * * *"
  key_expire:       "0 1 * * *"       # 置位过期 API Key
  quota_reset:      "0 0 * * *"       # 按平台时区推进 user_balances.quota_reset_at
  balance_notify:   "0 9 * * *"       # 余额低于阈值通知
  reserve_reclaim:  "*/15 * * * *"    # 冻结额度泄漏补偿（见 06 §3.3）

billing:
  reserve_buffer_ratio: 1.5           # 自适应预扣的放大系数
  min_reserve_output_tokens: 256
  default_currency: "USD"
  max_cost_per_request: 10.0          # 单请求费用上限，超出仍计费但立即告警（见 06 §11.5）
  reserve_ttl: 60m                    # 冻结额度的审计 TTL（> reclaim 阈值 45m > 结算最大重试时长约 41min，见 06 §4.1/§3.3）
  balance_notify_threshold: 10.0      # 余额低于此值触发通知（由 balance:notify 任务执行）

notification:                         # 余额通知与运营告警的通知渠道
  email:
    enabled: true
    smtp_host: ${SMTP_HOST}
    smtp_port: 587
    smtp_user: ${SMTP_USER}
    smtp_password: ${SMTP_PASSWORD}
    from: "noreply@cloudfog.example"
  webhook:                            # 可选：转发到企业 IM（飞书/钉钉/企业微信）
    enabled: false
    url: ${NOTIFY_WEBHOOK_URL}

payment:
  providers:                          # 密钥走环境变量
    # 注意：flow 映射内的 ${VAR} 必须加引号——裸写时内层 { 会破坏 YAML 语法
    alipay: { enabled: true,  app_id: "${ALIPAY_APP_ID}" }
    wechat: { enabled: true,  mch_id: "${WECHAT_MCH_ID}" }
    stripe: { enabled: false, secret_key: "${STRIPE_SECRET_KEY}" }
  order_expire: 30m
  notify_rate_limit: 100              # 每分钟每渠道回调限流

observability:
  log_level: info
  log_format: json
  metrics_enabled: true
  trace_enabled: false
  trace_sample_ratio: 0.01

limits:                                # 与 05 §12 对齐；限流细节见 05 §5.2
  global_concurrency: 10000
  per_ip_rpm: 600
  auth_fail_per_min: 20                # 同 IP 鉴权失败限流（08 §3.3）
  auth_fail_lock_minutes: 15
```

### 3.3 `settings` 表运行时配置清单

> 各章正文引用过、但**不属于启动配置**的运营可调项，统一归口到 `settings` 表（管理端可改，热更新机制见 [01 §11.2](./01-architecture.md#112-运行时配置热更新)）。这是它们的唯一权威清单——修改时需同步出处章节。

| key | 默认值 | 出处 | 说明 |
| --- | --- | --- | --- |
| `billing_timezone` | `Asia/Shanghai` | [02 §13](./02-data-model.md#13-时区与账期切分) | 账期切分平台时区；**变更需停服重算聚合** |
| `billing_precision` | `6` | [02 §14.2](./02-data-model.md#142-计费精度与舍入规则唯一标准) | 计费精度（小数位） |
| `exchange_rates` | `{"USD":1.0}` | [02 §14.4](./02-data-model.md#144-充值与赠送的入账) | 多币种汇率（订单另存当次快照） |
| `registration_enabled` | `true` | 注册开关 | 关闭后禁止自助注册 |
| `default_group_id` | — | 新用户默认分组 | 引导时写入 |
| `admin_adjust_limit_per_op` | `100` | [13 §4.2](./13-operations.md#42-操作审计) | 普通管理员单笔调账上限（USD） |
| `admin_adjust_limit_daily` | — | [13 §4.2](./13-operations.md#42-操作审计) | 每日调账总额上限 |
| `audit_body_sample_ratio` | `0.01` | [08 §4.3](./08-security.md#43-日志内容红线) | 请求体摘要审计采样率 |
| `usage_log_retention_months` | `6` | [02 §9.2](./02-data-model.md#92-生命周期) | 明细热数据保留月数 |
| `risk_*` | — | [13 §7](./13-operations.md#7-风控运营) | 风控阈值族（异常消费速率等） |
| `notice_*` | — | 通知模板 | 邮件/IM 模板文案 |

> 余额通知阈值同时存在于上方 YAML（`billing.balance_notify_threshold`）与本表，**以本表为准**（运营需要随时调整）；YAML 值仅作引导默认值写入 settings。

### 3.4 密钥管理建议

| 环境 | 方案 |
| --- | --- |
| 本地开发 | `.env` 文件（加入 `.gitignore`） |
| 单机部署 | 环境变量或 Docker secrets |
| 生产（推荐） | 密钥管理服务（Vault / 云厂商 KMS / Secrets Manager），启动时注入环境变量 |
| Kubernetes | Secret + External Secrets Operator |

**引导相关的环境变量**（[01 §10.3](./01-architecture.md#103-关键约束) 引用，此前未在本表登记）：

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `CLOUDFOG_ADMIN_EMAIL` | 否 | 首次引导创建的超管邮箱；未设置则用默认值并在日志中提示 |
| `CLOUDFOG_ADMIN_PASSWORD` | 否 | 首次引导的超管密码；**未设置则随机生成并仅在首次启动日志打印一次** |
| `CLOUDFOG_MASTER_KEY` | **是** | 主加密密钥（hex 32 字节），缺失时进程启动失败 |
| `CLOUDFOG_API_KEY_SALT` | **是** | API Key 哈希全局盐，缺失时启动失败 |

### 3.5 技术基线与核心依赖清单

> B1 开工必须先定基线，否则 `go.mod` 无法落笔。版本策略：**最低版本由 `go.mod` 声明**，CI 用 `go-version-file` 读取；依赖升级走 Renovate + `govulncheck` 门禁（[12 §5.1](./12-testing.md#51-门禁标准)）。下表给出的具体版本号为第 13 轮修订（2026-09-06）时的基线快照，作为 `go.mod` 初值与 CI 断言依据。

| 项 | 基线 | 说明 |
| --- | --- | --- |
| **Go** | **1.27.1**（最新稳定版，精确锁定） | `go.mod` 写 `go 1.27.1`，CI 用 `go-version-file: backend/go.mod` 读取；升级需同步 CI 版本断言与 Dockerfile 构建镜像（§2.1 的版本连锁三处清单） |
| Web 框架 | `gin-gonic/gin` | 路由与中间件链 |
| ORM | **`gorm.io/gorm v1.31.2`** + `gorm.io/driver/postgres`（pgx 驱动） | 模型手写无 codegen；金额 `Decimal` 直通（[02 §15.3](./02-data-model.md#153-金额字段的标准写法)）；**禁用 `AutoMigrate`**（[02 §11](./02-data-model.md#11-迁移与版本管理)） |
| 数据库迁移 | **`golang-migrate/migrate v4`**（library 嵌入，`cloudfog migrate` 子命令） | versioned SQL 是表结构唯一事实源（[02 §11](./02-data-model.md#11-迁移与版本管理)） |
| 异步队列 | **`github.com/rabbitmq/amqp091-go` v1.12.0**（AMQP 0-9-1 官方客户端） | 计量/结算/周期任务的投递与消费（[01 §7](./01-architecture.md#7-异步层设计)） |
| 周期调度 | `robfig/cron/v3`（进程内 cron） | scheduler 角色承载，发布周期任务到 RabbitMQ（[01 §7.3](./01-architecture.md#73-可靠性设计)） |
| Redis 客户端 | `redis/go-redis/v9` | 缓存、限流 Lua、分布式锁（队列已迁出） |
| 金额 | `github.com/shopspring/decimal` | 全部金额运算，禁止 float64（[02 §14](./02-data-model.md#14-金额精度与舍入策略)） |
| 配置 | `spf13/viper` | yaml + 环境变量覆盖 |
| 日志 | `log/slog`（标准库） | 结构化 JSON，`ReplaceAttr` 脱敏钩子 |
| 指标 | `prometheus/client_golang` | `/metrics` |
| 测试 | `stretchr/testify` + `testcontainers-go` | 断言与容器化集成测试（[12 §4](./12-testing.md#4-关键测试工具与基础设施)）；容器镜像与生产一致 |
| Lint | `golangci-lint v2` | CI 门禁 |
| 前端 | **Nuxt 4.2** + Node 24 LTS（pnpm） | 门户页 SSR/SEO，控制台/管理端 CSR（[14 §1.2](./14-frontend.md#12-技术栈)） |
| 容器基线 | Docker Engine（最新稳定版）+ Compose v2 插件 | 基础镜像：`postgres:18-alpine`、`redis:8.10-alpine`、**`rabbitmq:4.2-management`**、`golang:1.27.1-alpine`、**`node:24-alpine`**、`alpine:3.24`；全容器化交付（§2.1） |

**存储版本说明**：PostgreSQL 18（当前最新稳定大版本）；Redis 8.10（Redis Open Source 最新稳定版，2026-07 发布）；RabbitMQ 4.2（当前最新稳定大版本 4.2.x，镜像锁定 `rabbitmq:4.2-management`，管理插件预启用）。测试环境（testcontainers）必须使用与生产相同的镜像 tag，禁止"测试用旧版、生产用新版"的漂移。

**明确不引入**（避免依赖蔓延）：ORM 之外的 SQL 构建器、运行时 DI 容器（Wire 是代码生成）、goroutine 池库（消费并发由 RabbitMQ 消费者组 + prefetch 承载，见 [01 §7](./01-architecture.md#7-异步层设计)）、`rabbitmq_delayed_message_exchange` 社区插件（不随官方镜像分发，延迟/重试用原生 TTL+DLX，见 01 §7.1）。

---

## 4. 进程角色与扩缩容

### 4.1 角色说明

| 角色 | 职责 | 是否有状态 | 扩缩容 |
| --- | --- | --- | --- |
| `api` | HTTP 服务、Task Publisher（AMQP，只投递） | 无状态 | 按 CPU / 延迟扩缩 |
| `worker` | Task Consumer（消费任务） | 无状态 | 按队列深度扩缩 |
| `scheduler` | 进程内 cron，发布周期任务 | 无状态 | **固定 1 实例** |
| `all` | 以上全部（仅小部署/开发用） | — | 不扩缩 |

### 4.2 为什么 Scheduler 必须单实例

RabbitMQ 没有内置调度器，周期任务由 scheduler 进程内 cron（`robfig/cron/v3`）触发并 publish。cron 注册是幂等的（同一 cron 规范重复注册不会产生重复任务），但多实例会：

- 重复 publish 周期任务，产生不必要的消息与下游幂等开销；
- 若未来改用非幂等的调度实现，会直接产生重复任务。

因此固定 1 实例，并由容器编排保证重启（无需选主，重启期间最多漏一次调度，由任务自身的补偿逻辑兜底）。

### 4.3 扩缩容策略

| 角色 | 扩容信号 | 缩容保护 |
| --- | --- | --- |
| API | CPU > 60% 持续 5 分钟；在途请求数持续高位 | 缩容前等待在途请求归零（优雅关闭） |
| Worker | `task.critical` 队列 ready 深度（`rabbitmq_queue_messages_ready`）> **500** 持续 3 分钟（**扩容**阈值） | 缩容前等待在途消息 ack 完成（drain） |

> **与告警阈值的区分**：扩容 500（3 分钟）、告警 1000（5 分钟，P1，见 [09 §5.2](./09-observability.md#52-告警规则清单)）、规模化触发"常态 > 1000"（见 [11 §5.1](./11-roadmap.md#51-触发条件)）。三档数值不同但语义不冲突，实现时不要共用同一个常量。

**优雅关闭流程**：

```mermaid
sequenceDiagram
    participant K as 编排系统
    participant P as 进程
    participant LB as 负载均衡

    K->>LB: 摘除该实例
    K->>P: SIGTERM
    P->>P: 停止接受新请求（/readyz 返回失败）
    P->>P: 等待在途请求完成（上限 30s）
    P->>P: 关闭 Task Publisher（等待 publisher confirm 清空）
    P->>P: 关闭 HTTP Server、释放 DB/Redis/AMQP 连接
    P->>K: 退出（退出码 0）
    Note over K: 超过 shutdown_timeout 则 SIGKILL
```

Worker 进程收到信号后先 **cancel consumer 停止取新消息**，等待在途消息处理完成并 ack（上限 `shutdown_timeout`）；未 ack 的消息由 broker 自动重新入队，不会丢失。

---

## 5. 数据库运维

### 5.1 迁移

```bash
# 上线前执行（CI 或手动）
cloudfog migrate up          # 应用迁移
cloudfog migrate status      # 查看状态
```

| 规则 | 说明 |
| --- | --- |
| 时机 | 新版本部署前，独立步骤执行（不与滚动发布混在一起） |
| 兼容性 | 迁移必须向后兼容（先加列，后删列），支持新旧版本并存运行 |
| 大表 | 加索引用 `CONCURRENTLY`；加列允许 NULL 且不带默认值 |
| 回滚 | 每个迁移提供 down；生产优先前滚修复 |
| 备份 | **迁移前必须备份** |

### 5.2 备份

| 对象 | 方式 | 频率 | 保留 |
| --- | --- | --- | --- |
| PostgreSQL | `pg_dump`（自定义格式）或基础备份 + WAL 归档 | 每日全量 + 持续 WAL | 30 天 |
| Redis | AOF（appendonly yes）+ RDB 快照 | 持续 | 7 天 |
| RabbitMQ | 元数据（vhost/用户/队列拓扑）以"拓扑即代码"声明管理（01 §7.1，随版本管理）；**持久化消息不做备份**——broker 故障由 WAL 降级 + PG 幂等重建兜底 | — | — |
| 配置文件 | 纳入版本管理（不含密钥） | 随变更 | 永久 |
| 冷存日志 | 对象存储（已自带冗余） | — | 按策略 |

**备份必须加密**（含主密钥之外的独立密钥），并定期演练恢复（每季度一次）。

### 5.3 恢复演练要点

1. 在隔离环境用备份恢复，验证数据完整性（行数、关键表校验和）。
2. 验证应用能正常启动并读写。
3. 记录 RTO（恢复时间目标）与 RPO（恢复点目标），与业务要求比对。
4. **特别验证**：RabbitMQ 容器重启后 durable 队列与未 ack 的持久化消息是否完整保留并重新投递成功；WAL 降级重放是否幂等；`user_balances` 与 `billing_ledger` 是否一致。

---

## 6. 发布流程

### 6.1 滚动发布

```mermaid
flowchart LR
    A["1 构建镜像 + 打 tag"] --> B["2 执行数据库迁移"]
    B --> C["3 发布 Worker 节点<br/>（先升级消费者）"]
    C --> D["4 发布 Scheduler"]
    D --> E["5 滚动发布 API 节点<br/>逐台摘除→升级→就绪后接流量"]
    E --> F["6 观察 15 分钟核心指标"]
    F -->|异常| G["回滚"]
    F -->|正常| H["完成"]
```

**为什么先升级 Worker**：新版本可能投递新格式的任务负载。先升级消费者，保证新任务能被正确消费；反之若先升级 API，旧 Worker 可能无法解析新任务。

### 6.2 灰度与回滚

| 层级 | 灰度方式 |
| --- | --- |
| 实例级 | 先升级 1 台 API，观察 15 分钟无异常再全量 |
| 渠道级 | 新增供应商先配单渠道 + 小流量（通过 priority 控制），观察 24h |
| 功能级 | 通过 `settings` 表中的 feature flag 控制（如降级续写、内容审核） |

**回滚策略**：

| 场景 | 回滚方式 |
| --- | --- |
| 应用版本问题 | 重新部署上一版本镜像（最快） |
| 数据库迁移问题 | 不回滚数据库；修复后前滚（down 迁移仅在必要时使用，需停服） |
| 配置问题 | 改回配置并热更新（若支持）或重启 |
| 渠道问题 | 管理端直接禁用该渠道（无需发版） |

### 6.3 发布检查清单

- [ ] 单元测试 + 集成测试通过
- [ ] 迁移脚本已在预发环境验证
- [ ] 迁移前已备份数据库
- [ ] 配置项变更已同步到部署环境（含新增环境变量）
- [ ] 监控看板已就位，告警规则已更新
- [ ] 回滚方案已明确，上一版本镜像可用
- [ ] 发布窗口避开业务高峰

---

## 7. 容量规划参考

| 日调用量 | API 节点 | Worker 节点 | PostgreSQL | RabbitMQ | Redis |
| --- | --- | --- | --- | --- | --- |
| < 10 万 | 1（all 角色） | — | 2C4G，50GB | 1GB 磁盘 | 1GB |
| 10 万 ~ 100 万 | 2 × (2C4G) | 1 × (2C4G) | 4C8G，200GB | 1C2G，10GB 磁盘 | 2GB |
| 100 万 ~ 500 万 | 4 × (4C8G) | 2 × (2C4G) | 8C16G，500GB + 只读副本 | 2C4G，50GB 磁盘 | 4GB |
| > 500 万 | 按指标扩缩 | 按队列扩缩 | 16C32G + 读写分离 + 分区优化 | 3 节点 quorum 集群 | 8GB 或集群 |

> 以上为经验值，实际以压测结果为准。瓶颈通常先出现在 **PostgreSQL 写入**（日志落库）与 **RabbitMQ 磁盘写入**（消息持久化），而非 CPU。

---

## 8. 监控接入清单

| 组件 | 接入方式 |
| --- | --- |
| 应用 | `/metrics`（Prometheus 抓取） |
| Frontend（Nuxt SSR） | 容器健康检查 + TTFB 合成探测（公开页 < 200ms，见 [14 §8.7](./14-frontend.md#87-性能要点)） |
| RabbitMQ | Prometheus 插件（15692 端口）+ Management UI（15672） |
| PostgreSQL | `postgres_exporter` |
| Redis | `redis_exporter` |
| 主机 | `node_exporter` |
| 容器 | cAdvisor 或云厂商监控 |

---

## 9. 参考来源（sub2api）

| 本设计点 | 参考位置 | 关系 |
| --- | --- | --- |
| 单二进制多职责 | `backend/cmd/server`、`backend/Dockerfile` | 沿用 |
| 配置分模块结构 | `backend/internal/config/config.go`（含 `Gateway.UsageRecord.WorkerCount/QueueSize/OverflowPolicy/AutoScale*`） | 沿用分模块配置；本平台改为 RabbitMQ 队列配置 |
| 溢出策略与降级 | 同文件 + `internal/service/usage_record_worker_pool.go`：`OverflowPolicySync` 默认、"关停窗口丢弃应降级为同步执行兜底"的设计取舍 | 沿用取舍原则：账单不可丢 |
| 自动扩缩容参数 | 同文件：worker 128~512、队列占用率 70% 扩容 / 15% 缩容、步长 32/16、冷却 10s | 参考数值区间，本平台由 RabbitMQ 消费者并发配置 + 外部 HPA 替代 |
| 部署脚本 | `deploy/`（Docker Compose、systemd service、shell 脚本） | 参考组织方式；本平台**全容器化**，弃用 systemd 部署路径（§2.1） |
| 备份恢复 | `internal/service/backup_service.go`、`backup_archive.go`、`internal/handler/admin/backup_handler.go` | 作为扩展位：管理端一键备份 |
| 系统更新与操作锁 | `internal/service/update_service.go`、`system_operation_lock_service.go` | 参考：危险操作加全局锁 |