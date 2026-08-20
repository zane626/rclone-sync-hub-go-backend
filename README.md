# Rclone Sync Hub

面向生产环境的本地文件扫描与 rclone 上传调度服务。系统扫描多个监听目录，把新文件或发生变化的文件持久化为上传任务，通过 MySQL 租约安全地并发执行，并提供 Vue 3 管理界面、审计、指标和运维工具。

## 已实现功能

| 模块 | 功能 |
|---|---|
| 监听目录 | 多目录配置、独立扫描周期、深度和关键字过滤、启停、立即扫描 |
| 增量扫描 | 文件大小 + 纳秒 mtime 指纹、稳定窗口、快照批量写入、变更版本重新建单、删除文件标记 |
| 扫描可靠性 | `next_scan_at` 调度、目录超时、失败自动恢复、扫描历史、目录级并行、分布式租约和心跳 |
| 上传队列 | MySQL 持久化队列、`FOR UPDATE SKIP LOCKED` 领取、优先级、租约续期、进程崩溃恢复 |
| 失败处理 | 指数退避 + 抖动、最大次数、任务超时、暂停、取消、单个与批量重试 |
| rclone | 精确文件目标 `copyto`、结构化进度、输出限长、remote 白名单、配置列表脱敏 |
| 一致性 | 幂等键防重；任务成功与文件版本上传标记在同一数据库事务中提交 |
| 权限安全 | HMAC Bearer 登录、bcrypt 密码、admin/viewer 角色、登录限流、请求体限制、安全响应头 |
| 资源边界 | 本地根目录和 rclone remote 白名单、符号链接解析、路径穿越防护、变更操作审计 |
| 可观测性 | JSON 日志、请求 ID、liveness/readiness、Prometheus 指标、扫描逾期/队列积压告警、扫描历史、审计日志、SSE 进度 |
| 数据治理 | 版本化迁移、迁移锁和 dirty 检测、日志/终态任务/扫描/审计数据分批保留清理 |
| 交付运维 | 非 root 只读镜像、Compose 安全基线、CI 测试/漏洞门禁、多架构镜像、SBOM/来源证明 |
| 灾备 | MySQL 全量备份、SHA-256 校验、恢复保护开关、binlog/PITR 基线、systemd 定时器示例 |

## 为什么长时间运行后不会停止扫描

扫描是否到期由数据库中的 `next_scan_at` 决定，不依赖一次性内存定时器。无论成功、目录暂时不可用、超时还是进程退出，扫描状态都会被终结或由过期租约恢复。`error` 状态不会被永久排除；多实例同时运行时，同一目录也只会被一个扫描器领取。

扫描热路径不再对每个文件查询数据库或调用一次 rclone。每轮先批量读取快照和未完成任务，再遍历文件系统；首次发现以及后续批量变化的快照和任务都按批写入。不同目录可以受控并行，同一目录仍顺序遍历，避免磁盘随机 I/O 失控。

“立即扫描”只会把全部启用目录持久化为到期状态并唤醒扫描器，接口立即返回 `202 Accepted`，不会让 HTTP 请求等待完整文件遍历。实际结果和耗时通过扫描历史查看。

默认指纹使用文件大小和纳秒 mtime，不读取完整文件内容。这是扫描性能与强内容校验之间的明确取舍：如果外部程序可能在保持大小和 mtime 完全不变的情况下修改内容，应在业务流程中保留不可变落盘约束，或另行安排低频 checksum 校验，而不要把全量哈希放进高频扫描。

## 技术栈

- Go 1.25 语言基线，生产构建工具链 Go 1.26.7
- Gin、GORM、MySQL 8.4、zap、Prometheus client
- Vue 3、Vue Router、原生 CSS 设计系统、Vite 8、pnpm（不依赖 UI 组件框架）
- rclone 1.75.0
- Docker/Compose、GitHub Actions

详细依赖关系和状态机见 [ARCHITECTURE.md](ARCHITECTURE.md)，上线、备份、恢复和告警流程见 [docs/OPERATIONS.md](docs/OPERATIONS.md)。

## 快速开始

### 生产方式：Docker Compose

1. 复制 `.env.example` 为 `.env`，替换所有占位密码。管理员密码至少 12 字符，签名密钥和监控令牌至少 32 字符。
2. 在 `RCLONE_CONFIG_DIR` 下放置 `rclone.conf`；把待上传数据放在或挂载到 `LOCAL_DATA_DIR`。源数据挂载为只读；OAuth remote 的配置目录需允许容器 UID 10001 写回刷新后的 token。
3. `ALLOWED_RCLONE_REMOTES` 只填写允许使用的 remote 名称，多个名称用逗号分隔。
4. 校验并启动：

使用默认路径的 Linux 主机可执行 `chown -R 10001:10001 ./rclone && chmod 700 ./rclone`；自定义路径时先确认目标再调整命令。

```sh
docker compose config
docker compose up -d --build
docker compose ps
```

默认只绑定 `127.0.0.1:8080`。公网访问必须通过 TLS 反向代理；示例位于 `deploy/nginx/rclone-sync-hub.conf.example`。

健康检查：

- `GET /api/health/live`：进程存活，不依赖数据库
- `GET /api/health/ready`：数据库可用，可接收业务流量

### 本地开发

需要 Go、Node 24、pnpm、rclone 和 MySQL 8.4。

```sh
docker compose -f docker-compose.dev.yml up -d
make frontend-install
CONFIG_PATH=configs/config.dev.yaml go run ./cmd/server
```

另开终端启动前端开发服务器：

```sh
pnpm --dir frontend dev
```

浏览器访问 `http://127.0.0.1:5173`。开发配置不嵌入占位静态页；Vite 通过 `VITE_API_PROXY_TARGET` 代理 API，默认目标是 `http://127.0.0.1:8080`。生产镜像会在多阶段构建中把真实前端产物注入 Go 二进制。

## 核心配置

配置优先级为环境变量覆盖 YAML。生产 Compose 会强制要求数据库密码、管理员密码、签名密钥和 remote 白名单。

| 类别 | 关键配置 |
|---|---|
| 扫描 | `SCAN_INTERVAL_SECONDS`、`SCAN_FOLDER_TIMEOUT_SECONDS`、`SCAN_FILE_STABLE_SECONDS` |
| 扫描性能 | `SCAN_MAX_CONCURRENT_FOLDERS`、`SCAN_BATCH_SIZE` |
| 扫描恢复 | `SCAN_LEASE_SECONDS`、`SCAN_HEARTBEAT_SECONDS` |
| Worker | `WORKER_MAX_CONCURRENT`、`WORKER_TASK_TIMEOUT_SECONDS`、重试与租约参数 |
| 数据库 | 连接池、连接寿命、connect/read/write timeout、可选 `DB_TLS` |
| 安全 | `AUTH_*`、`ALLOWED_LOCAL_ROOTS`、`ALLOWED_RCLONE_REMOTES`、`TRUSTED_PROXIES` |
| 运维 | `METRICS_BEARER_TOKEN`、保留天数、`BACKUP_DIR`、资源上限 |

错误的布尔值或数值环境变量、YAML 未知字段，以及不可读取的已配置文件都会让程序启动失败，不会静默退回默认值。

## 主要 API

公开接口：

- `GET /api/health/live`
- `GET /api/health/ready`
- `GET /api/auth/config`
- `POST /api/auth/login`

登录后可用：

- 任务：`/api/tasks`、`/api/tasks/:id/logs`、`/api/stats`
- 分析：`/api/analytics/dashboard`
- 监听目录：`/api/watch-folders`
- 扫描历史：`/api/scan-runs`
- 实时事件：`/api/events`
- rclone remote：`/api/rclone/configs`

管理员写接口包括任务创建/重试/暂停/取消/删除、批量操作、异步立即扫描（`POST /api/scan`）、监听目录管理和审计日志。`/metrics` 使用独立监控 Bearer token；未配置时要求管理员 token。Swagger 只建议在开发环境开启。

## 验证与发布门禁

```sh
go test ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
pnpm --dir frontend audit --prod --audit-level=high --registry=https://registry.npmjs.org
pnpm --dir frontend build
docker compose --env-file .env.example config --quiet
```

CI 还会运行 Go race detector、真实 MySQL 事务/幂等集成测试以及生产镜像构建。Tag `v*` 只有在验证通过后才发布 amd64/arm64 镜像，并生成 SBOM 与构建来源证明。

## 备份与恢复

```sh
docker compose --profile operations run --rm backup
```

恢复会覆盖目标数据库，必须先停应用并显式设置 `ALLOW_RESTORE=YES`：

```sh
docker compose stop app
RESTORE_FILE=/backups/rclone_sync_hub_YYYYMMDDTHHMMSSZ.sql.gz ALLOW_RESTORE=YES docker compose --profile operations run --rm restore
docker compose up -d app
```

本机备份目录不是异地灾备。必须把备份和需要的 binlog 复制到独立存储，并定期在隔离环境做恢复演练。

## 当前边界

- 本项目是单向 `local_to_remote` 上传，不执行远端删除，也不是双向同步工具。
- MySQL 是持久化协调中心；生产高可用需要使用受管 MySQL 或自行建设复制、备份与故障切换。
- 多应用实例必须看到相同的本地路径和 rclone 配置。SSE 是进程内事件流，前端每 30 秒会用数据库结果校准一次。
- remote 的人工删除不会在高频本地扫描中逐文件探测；这是为了避免远端 API 调用再次成为扫描瓶颈。
