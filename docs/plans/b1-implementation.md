# B1 工程地基实施计划

> 状态：进行中（2026-09-06 启动）。本文是实施排期记录，设计依据仍以 docs/01~14 为唯一事实源；实施中发现的设计偏差回写对应文档。
>
> **进度**：阶段 0 ✅（仓库结构 / go.mod + go.sum / Makefile / CI / dev compose / enabled_plugins / .env.example / .gitignore，依赖解析验证通过；自审修正 asynq 残留、补 testcontainers、补 .golangci.yml）→ 阶段 1 ✅（2026-09-06：internal/config 全量 Viper 配置 + 16 个扁平 env 别名 + Validate 启动校验含消费者并发单调铁律、internal/pkg/logger JSON/UTC/脱敏、internal/model/db.go pgx 连接与连接池；BUILD/VET/TEST 全过）→ 进行中：阶段 2。
> 批次定义见 [11-roadmap §3.1](../11-roadmap.md)；预计 5~7 个工作日。

## 阶段 0：仓库与工具链初始化（0.5 天）

| 交付物 | 要点 |
| --- | --- |
| 仓库结构 | `backend/`（Go 模块根）、`deploy/`、`docs/` |
| backend/go.mod | module `cloudfog`，go 1.27.1；gin / viper / gorm v1.31.2 + driver/postgres / golang-migrate v4 / amqp091-go v1.12.0 / go-redis/v9 / robfig/cron/v3 / shopspring/decimal / prometheus/client_golang |
| Makefile | dev / build / test-unit / test-integration / lint / migrate-up / compose-up |
| CI workflow | golangci-lint v2 + go vet + test + build；gitleaks、govulncheck（12 §5.1） |
| deploy/docker-compose.dev.yml | PG18 / Redis 8.10 / RabbitMQ 4.2-management + healthchecks + enabled_plugins |
| .env.example | 10 §1.3 清单（含 RABBITMQ_USER/PASSWORD） |

验收：三容器 healthy；`make lint` 通过。

## 阶段 1：配置与基础设施（1 天）

- `internal/config`：Viper 全量配置（10 §3.2 全部段）；必填校验（缺 DSN/主密钥/盐/RabbitMQ URL 即失败）；环境变量扁平别名
- `internal/pkg/logger`：slog JSON + ReplaceAttr 脱敏钩子（Redactable）+ UTC RFC3339
- `internal/model/db.go`：gorm.Open（pgx）+ 连接池参数

## 阶段 2：数据层 + 异步层（2~3 天，B1 核心）

- `internal/model/` 27 张表 GORM 模型（02 §3~§8、§16；Channel/UsageLog 对照 §15.4）
- `migrations/0001_init`：全部建表 SQL（usage_logs 分区 + 复合主键、部分唯一索引、CHECK）+ down
- golang-migrate library 集成：`cloudfog migrate up/down/status`
- `internal/task/`：拓扑声明（01 §7.1）、TaskEnqueuer（confirm + mandatory + delivery mode 2）、consumer runner（手动 ack、retry TTL 桶、x-retry-count、DLQ）、优雅关闭

验收：干净库 up→down 1→up 幂等；集成测试覆盖"投递→消费→失败→重试桶→回投→成功"。

## 阶段 3：HTTP 服务与引导（1~2 天）

- `internal/server`：Gin + recovery + 请求日志 + /healthz /readyz /metrics（占位）+ 优雅关闭（10 §4.3）
- `cmd/cloudfog`：--role=api|worker|scheduler|all；migrate 子命令
- `internal/setup/bootstrap`：迁移检查 → advisory lock → 种子（01 §10.3）→ bootstrap_completed

## 阶段 4：测试与 B1 验收（1 天）

- 单元测试：config、logger 脱敏、task 重试桶/header
- 集成测试（testcontainers，镜像与生产同版本）：拓扑幂等、投递消费、重试→死信→reclaim
- **B1 验收（11 §3.1）**：`make dev` 起得来；/healthz 200；PG 20+ 表；CI 全绿

## 明确不做（B1 范围外）

- Nuxt 前端脚手架 → B4；中间件链/IR/Adapter/计费任务 → B2；管理端 API → B3

## 纪律

- 实施偏差当天回写 docs 对应文档
- 开工前确认无并行会话占用 backend/
