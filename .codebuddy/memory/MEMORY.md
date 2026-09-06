# 云之雾（CloudFog）项目 · 长期记忆

> 最后更新：2026-09-06 第 21 轮审计收敛后。每日过程细节见 `2026-09-06.md`，本文件只存最终状态与持久规则。

## 用户偏好（必须遵守）

- **前端精致度要求很高**："页面的精致程度要求很高，毕竟这是要给用户使用的"。用户控制台质感对标 Vercel/OpenAI 控制台；Element Plus 仅作基础、必须按 design tokens 深度主题定制；金额/token 等宽数字右对齐；暗色模式一等公民；14 §8 设计系统与 §11 验收（F11~F16）是硬验收。
- 交付期望：能交付落地的应用，不是 demo；文档闭环严谨、可工业级实施。
- 文档纪律：关键链路跨文档闭环；重要设计"唯一权威出处 + 其他处引用"；实现偏差回写文档（文档即事实源）。
- 用户指定（2026-09-06）：ORM 用 **GORM**（替代 Ent）；消息队列用 **RabbitMQ**（`rabbitmq:4-management`）；部署**全面 Docker 容器化**（含本地开发，systemd 弃用）；前端 **Nuxt 最新版**（SEO）。

## 文档集状态

- `api/README.md` + `docs/01~14`，**当前 v1.3（2026-09-06，第 21 轮审计收敛，零修改）**
- 21 轮审计覆盖：结构、数据字典（正/反向）、编号锚点（脚本全量校验）、数值与时序、接口契约与 OpenAPI 归属、配置与拓扑交叉、场景/链路走查、字段级投影一致性、mermaid 全量清查、部署与回归覆盖
- 第 13 轮起的大改：Ent→GORM、Asynq→RabbitMQ、PG18/Redis8.10/Go1.27.1、全容器化、前端 Vue3→Nuxt 4.2
- 累计拦截 30+ 实现级问题（含资金级 3 个：TTL 桶死信路由键丢失、L2 时机漏结算、幂等记录前置写）

## 技术基线（最终）

| 项 | 基线 |
| --- | --- |
| Go | 1.27.1 精确锁定（golang:1.27.1-alpine） |
| ORM | GORM v1.31.2 + gorm.io/driver/postgres（pgx）；模型手写 `internal/model/`；金额 Decimal 直通；**全面禁用 AutoMigrate** |
| 迁移 | golang-migrate v4（library 嵌入 `cloudfog migrate` 子命令）；SQL 唯一事实源 |
| 主存储 | PostgreSQL 18（postgres:18-alpine）；usage_logs 按月分区（PARTITION BY RANGE (created_at)，主键含分区键） |
| 缓存/限流/锁 | Redis 8.10（redis:8.10-alpine），职责仅此三项，noeviction |
| 异步队列 | RabbitMQ 4.2（rabbitmq:4.2-management）+ amqp091-go v1.12.0 |
| 周期调度 | robfig/cron/v3 进程内 cron，scheduler 单实例 + Redis 分布式锁 |
| 前端 | Nuxt 4.2 + Node 24 LTS（node:24-alpine）；公开门户 SSR/SEO，console/admin CSR；routeRules 唯一权威；pnpm |
| 基础镜像 | postgres:18-alpine / redis:8.10-alpine / rabbitmq:4.2-management / golang:1.27.1-alpine / node:24-alpine / alpine:3.24 |

## RabbitMQ 异步层要点（01 §7.1 唯一权威）

- 拓扑：direct exchange `cloudfog.tasks`（routing key=任务类型，19 个任务类型见 01 §7.2）→ `task.critical/default/low`（MVP 单节点 durable classic，规模化 quorum）→ DLX `cloudfog.dlx`（fanout）→ `task.dlq`
- 重试/延迟桶：**必须经 fanout ingress 交换机**（`cloudfog.retry.<b>` / `cloudfog.delay.<d>`，routing key 仍填任务类型）——**禁止默认交换机直投桶队列**（死信时任务类型丢失 → broker 静默丢弃，资金级）
- 重试窗口推算：10 次经 `[5s,1m,5m]` ≈ **41min**；reclaim 阈值 45min、冻结审计 TTL 60min 均据此设定（改重试策略必须重算整条时序链）
- 死信重投：从 `x-death` 头恢复 routing key；max_retry 优先级：Task.MaxRetry>0 > 队列配置
- 镜像要点：-management 变体预启用管理插件；rabbitmq_prometheus 需 enabled_plugins 显式启用；`RABBITMQ_DEFAULT_*` 仅空卷首初始化生效；/var/lib/rabbitmq 卷必挂；端口 5672/15672/15692
- 不引入 rabbitmq_delayed_message_exchange 社区插件（TTL+DLX 替代）

## 公开门户 / SEO 要点（14 §1）

- 三区域：公开门户 `/`（SSR+swr 缓存）、`/console`（CSR）、`/admin`（CSR）；routeRules 唯一权威
- 公开数据走未鉴权端点：`GET /api/v1/public/models`（基础价，实际扣费按分组倍率）、`GET /api/v1/public/announcements`——OpenAPI 归属 `public.yaml`
- 渠道列表"状态"列是聚合派生（disabled > 熔断中 > 限流中 > error > 正常），02 只存原始字段

## 持久教训（审计与实现通用）

1. 架构替换必须全局重画 mermaid 拓扑图（文字改了图最容易漏——第 20 轮发现 10 §1.2 残留）
2. 改重试策略必须重算最大重试总时长，同步核对所有 TTL/补偿阈值（reserve TTL、reclaim、对账窗口）
3. TTL+DLX 桶的消息进入方式决定死信路由键是否保留（见上）
4. 新公开页面必须同步检查：数据来源端点是否未鉴权可用（公告/模型广场两例）+ OpenAPI 归属 + sitemap 是否收录
5. 审计方法沉淀：锚点脚本全量校验、正/反向覆盖查（定义→引用 与 引用→定义）、逐字段投影比对、参数交叉核对、时序推算

## 环境

- 本地开发：Docker compose（cloudfog-postgres/redis/rabbitmq，凭据与卷 cloudfog 前缀）
- Windows/PowerShell：psql 三坑（$ 转义、中文路径、用 127.0.0.1）；Node 侧 pnpm 非 npm
- 存在并行会话完善文档——修改前必须重读目标文件确认最新状态

## 下一步

- **B1 实施中**（计划与进度：`docs/plans/b1-implementation.md`）：阶段 0 ✅（自审+二轮机器验证修正：剔除 asynq 残留、补 testcontainers-go 与 .golangci.yml v2；compose/Makefile/插件文件/CI Action 均机器验证通过）→ 阶段 1 ✅ **九轮审查后定稿**（internal/config：14 个配置段全量 Viper + 16 个扁平 env 别名 + `${VAR}` 插值展开（**可选引用未设置→空串**，previous_master_key/webhook.url 语义；必填缺失由 Validate 兜底；注意 Viper 优先级 env>config 文件，必填键在 config.yaml 写 ${} 恒被 env 覆盖）+ Validate 含并发单调铁律/master_key hex/桶升序/cron fail-fast（robfig AddFunc 会 panic，Load 期 ParseStandard 校验，空=不启用）/新旧主密钥同值/observability 枚举/gateway·scheduling·circuit 正数与区间 + master_key_id 非空 + reserve_buffer_ratio≥1 + LoadDotEnv（真实 env 优先 + UTF-8 BOM 剥离 + godotenv 语义行内注释剥离——引号值取首尾同类引号之间，含 # 的密码须引号包裹）+ **黄金文件测试 Test_Load_DocYamlGolden**（10 §3.2 全文 YAML 原样可执行 + 31 键值断言）+ 16 个单元测试；internal/pkg/logger：slog JSON + UTC + 脱敏——六轮审查修复 6 个问题：① 7 个 env 别名键漏 SetDefault（Viper 陷阱）② Handle 未脱敏 Record 自身 Attrs ③ Redactable 优先级 ④ inner ReplaceAttr 二次掩码（职责切分：ReplaceAttr 只管时间）⑤ **敏感键精确匹配漏组合键**（smtp_password/stripe_secret_key 等，改精确集+后缀规则 *password/*secret/*secret_key/*token/*api_key）⑥ 回归测试 8 个；internal/model/db.go：pgx 连接 + 连接池 + **NowFunc UTC**（02 §13.1）+ Ping。BUILD/VET/TEST 全过）→ 阶段 2 进行中（子任务 s2-1 ✅：migrations/embed.go（go:embed * 单二进制内嵌）+ internal/migrate（golang-migrate v4.19.1 library，Up/Down/Status/RequireSchemaVersion，DSN 须 postgres://）+ cmd/cloudfog migrate up|down N|status，真实 PG18 端到端冒烟通过（container healthy + status 连库 exit 0）→ s2-2 internal/model 27 表 GORM 模型进行中）

**重大镜像陷阱（PG18 官方镜像，2026-09-06 发现并修复）**：PG18+ 镜像数据目录约定变更——必须挂载**父目录 `/var/lib/postgresql`**（数据落在其下 `18/docker` 子目录，支持 pg_upgrade --link）；挂旧路径 `/var/lib/postgresql/data` 会被判定为 "unused mount/volume" 而**拒绝启动**。已修 deploy/docker-compose.dev.yml 与 docs/10 §2 生产示例（含注释）。**教训：升级大版本镜像必须核对官方镜像的挂载路径约定变更**
- 开工提示：`go mod tidy` 会清空无 import 的 require 块——代码落地后再 tidy；**`-race` 在 Windows 本机需要 gcc（cgo）**，race 门禁由 CI（ubuntu）执行，本地用普通 `go test`；robfig/cron 已随阶段 1 代码加回（v3.0.1）
- B1 剩余：internal/model 27 表 + migrations/0001（分区/部分唯一索引/CHECK）+ internal/task 拓扑与 TaskEnqueuer + server/bootstrap + B1 验收（make dev / healthz 200 / 20+ 表 / CI 绿）
- 注意：文档对 sub2api `ent/schema/*.go` 的"参考来源"引用是外部项目事实路径，保留不改（02 §15 开头有澄清注记）
