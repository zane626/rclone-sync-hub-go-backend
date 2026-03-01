# Rclone Sync Hub（Go 后端）

文件上传调度系统：定时扫描本地目录与监听文件夹、判断是否已上传、入队、控制并发、调用 rclone 上传、解析进度、更新数据库，并提供 REST API 与 Vue3 管理界面。生产环境可嵌入前端构建产物，单二进制部署。

---

## 功能概览

| 功能 | 说明 |
|------|------|
| **监听文件夹（Watch Folders）** | 管理多个本地目录，每个目录可配置远程名称、远程路径、是否启用；定时扫描未上传文件并入队 |
| **任务调度** | 任务状态：pending → running → success / failed；Worker 池 + rclone 执行上传，进度与日志写入 `upload_logs` |
| **任务 CRUD** | 列表、详情、创建、重试、暂停、删除；支持批量重试、批量暂停、批量删除 |
| **任务日志** | 按任务 ID 查询上传进度日志（`GET /api/tasks/:id/logs`） |
| **数据分析** | 仪表盘接口：按状态/文件夹/时间聚合统计、任务列表（`GET /api/analytics/dashboard`） |
| **本地目录浏览** | 列出指定路径下的子目录（`GET /api/fs/subdirs`），便于前端选择扫描路径 |
| **rclone 配置** | 列出 rclone 已有远程配置名称（`GET /api/rclone/configs`） |
| **健康检查** | `GET /api/health` |
| **Swagger 文档** | 开发环境下可开启 `/swagger/index.html` |
| **前端嵌入** | Vue3 + Naive UI；History 模式；未匹配路径回退 `index.html`（SPA fallback） |

---

## 技术栈

- **Go 1.22+**，Gin，Gorm，**MySQL 8.0+**，zap，YAML 配置
- **前端**：Vue3、Naive UI
- 分层架构：api / service / repository / database / scheduler / worker / rclone / model / config / logger
- 接口解耦、依赖注入、无循环依赖；repository 为接口，可后期切换数据库实现

---

## 目录结构

```
cmd/server/              # 入口：main.go，embed frontend/dist
internal/
  api/                   # HTTP 层：路由与 handler，/api 前缀
  service/               # 业务编排：调用 repository、worker、scheduler
  repository/            # 数据访问接口与实现（每实体单独文件），仅此处依赖 Gorm
  database/              # 数据库连接与迁移（MySQL driver、连接池、AutoMigrate）
  scheduler/             # 定时扫描本地目录与 watch_folders，未上传文件入队
  worker/                # 任务队列：channel、最大并发、重试，仅通过 rclone 接口
  rclone/                # rclone 封装：唯一使用 exec 的模块，解析 --progress
  model/                 # 领域模型与表结构（task、file_record、upload_log、watch_folder）
  config/                # 配置加载（yaml + 环境变量）
  logger/                # zap 封装
frontend/                # Vue3 项目（单独开发，npm run build → frontend/dist）
cmd/server/frontend/dist/ # Vue 构建产物供 go:embed（Docker 多阶段会注入）
configs/                 # 配置文件（config.yaml、config.dev.yaml）
```

---

## 数据库（MySQL）

- 使用 **Gorm + MySQL driver**，连接与迁移封装在 `internal/database`；**repository 保持接口**，便于后期切换。
- 表结构由 **AutoMigrate** 在启动时自动迁移，无需手跑 SQL。
- **表**：
  - **upload_tasks**：任务（status: pending / running / success / failed，含 progress、retry_count、error_message、remote_name、remote_path 等）
  - **file_records**：文件路径、大小、是否已上传（用于去重）
  - **upload_logs**：上传进度日志
  - **watch_folders**：监听文件夹配置（本地路径、远程名称、远程路径、启用状态、最后扫描/同步时间等）

---

## 配置说明

### YAML 配置文件

主配置见 `configs/config.yaml`，包含：

- **server**：port、mode（debug/release）、embed_frontend、enable_swagger
- **database**：host、port、user、password、dbname、charset、max_open_conns、max_idle_conns、conn_max_idle_time_mins
- **scan**：local_path（要扫描的本地根目录）、enabled、interval_seconds（用于全局扫描与 watch_folders 定时扫描）
- **worker**：max_concurrent、max_retry、queue_size
- **rclone**：bin_path（默认 `rclone`）
- **log**：level（debug/info/warn/error）、format（json/console）

使用 docker-compose 时请将 `database.host` 改为 `mysql`。

### 环境变量（无配置文件时）

**不提供配置文件**时（如 Docker 仅用 environment），可从环境变量加载全部配置：

| 类别 | 环境变量示例 |
|------|------------------|
| 数据库 | DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME, DB_CHARSET, DB_MAX_OPEN_CONNS, DB_MAX_IDLE_CONNS, DB_CONN_MAX_IDLE_TIME_MINS |
| 服务 | SERVER_PORT, SERVER_MODE, EMBED_FRONTEND, ENABLE_SWAGGER |
| 扫描 | SCAN_LOCAL_PATH, SCAN_ENABLED, SCAN_INTERVAL_SECONDS |
| Worker | WORKER_MAX_CONCURRENT, WORKER_MAX_RETRY, WORKER_QUEUE_SIZE |
| 其它 | RCLONE_BIN_PATH, LOG_LEVEL, LOG_FORMAT |

布尔值可用 `true`/`false`、`1`/`0`。详见 `internal/config/config.go` 中的 `applyEnvOverrides`。

---

## 运行方式

### 1. 本地直接运行

1. 安装 **Go 1.22+**、**MySQL 8.0+**、**rclone**。
2. 创建数据库：
   ```bash
   CREATE DATABASE rclone_sync_hub CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
   ```
3. 复制并修改配置：
   ```bash
   cp configs/config.yaml configs/my.yaml
   # 编辑 database（host/port/user/password/dbname）、scan.local_path、scan.remote_name 等
   ```
4. 启动（自动执行 AutoMigrate）：
   ```bash
   go mod tidy
   CONFIG_PATH=configs/my.yaml go run ./cmd/server
   # 或默认读取 configs/config.yaml：go run ./cmd/server
   ```
5. 访问：API 基地址 `http://localhost:8080/api`，前端（若嵌入）`http://localhost:8080/`。

### 2. 本地开发（热重载）

适合日常开发：只起 MySQL，本机用 Fresh 跑 Go，改代码自动重启，默认使用 `configs/config.dev.yaml`（localhost MySQL + 开 Swagger + debug 日志）。

**安装 Fresh（仅需一次）**

```bash
go install github.com/zzwx/fresh@latest
```

**启动 MySQL**

```bash
make dev-mysql
# 或：docker compose up mysql -d
```

**启动热重载**

- **Linux / Mac / Git Bash**：`make dev` 或 `CONFIG_PATH=configs/config.dev.yaml fresh`
- **PowerShell**：`.\dev.ps1` 或 `$env:CONFIG_PATH="configs/config.dev.yaml"; fresh`
- **CMD**：`set CONFIG_PATH=configs\config.dev.yaml && fresh`

**访问**

- API：http://localhost:8080/api
- Swagger：http://localhost:8080/swagger/index.html

热重载行为由项目根目录 **`.fresh.yaml`** 控制（main_path、监听扩展名、排除目录等）。

### 3. Docker 构建与运行

**构建镜像**

```bash
docker build -t rclone-sync-hub .
```

多阶段：Node 构建 Vue → Go 构建二进制（含 swag 生成文档）→ 最终镜像 alpine + 安装 rclone。Vue 构建产物会复制到 `cmd/server/frontend/dist` 供 `go:embed` 使用。

**使用 docker-compose（推荐）**

- 项目名：`rclone-sync-hub`。
- 不挂载配置文件时，**全部通过 environment 配置**（见 `docker-compose.yml`）。
- 需挂载：本地待上传目录（如 `./data/local:/volumes:ro`）、rclone 配置目录（如 `~/.config/rclone:/root/.config/rclone:ro`）。

```bash
docker compose up -d
```

- 应用依赖 MySQL 健康后再启动。
- 生产环境请将 `ENABLE_SWAGGER` 设为 `false`。

---

## API 列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/health | 健康检查 |
| GET | /api/analytics/dashboard | 数据分析仪表盘（概览、按状态/文件夹/时间、列表） |
| GET | /api/stats | 各状态任务数量（pending/running/success/failed） |
| GET | /api/tasks | 任务列表（可选 status、page、page_size） |
| GET | /api/tasks/:id | 任务详情 |
| GET | /api/tasks/:id/logs | 任务上传进度日志 |
| POST | /api/tasks | 创建任务 |
| POST | /api/tasks/:id/retry | 重试任务（重新入队） |
| POST | /api/tasks/:id/pause | 暂停任务 |
| DELETE | /api/tasks/:id | 删除任务 |
| POST | /api/tasks/batch/retry | 批量重试 |
| POST | /api/tasks/batch/pause | 批量暂停 |
| POST | /api/tasks/batch/delete | 批量删除 |
| POST | /api/scan | 触发一次目录扫描 |
| GET | /api/rclone/configs | 列出 rclone 远程配置名称 |
| POST | /api/watch-folders | 创建监听文件夹 |
| GET | /api/watch-folders | 监听文件夹列表 |
| GET | /api/watch-folders/:id | 监听文件夹详情 |
| PUT | /api/watch-folders/:id | 更新监听文件夹 |
| DELETE | /api/watch-folders/:id | 删除监听文件夹 |
| GET | /api/fs/subdirs | 列出指定路径下的子目录（query: path） |

---

## Swagger 接口文档

- 使用 **swaggo/swag** 生成 OpenAPI，**gin-swagger** 提供 UI；注解写在 api 层。
- 访问：**`/swagger/index.html`**（仅当 `server.enable_swagger` 为 true 时生效）。
- **生产环境必须设为 false**。

**生成文档**

```bash
go install github.com/swaggo/swag/cmd/swag@latest
swag init -g cmd/server/main.go -o cmd/server/docs --parseDependency --parseInternal
```

或使用 Makefile：`make swagger`（安装 swag + 生成）、`make swagger-only`（仅生成）。

Docker 构建时已包含「安装 swag → swag init → go build」，镜像内带最新文档；是否暴露 UI 仍由运行时配置决定。

---

## 前端（Vue3）

- **技术**：Vue3、Naive UI、Vue Router（History 模式）。
- **嵌入**：使用 `go:embed` 嵌入 `cmd/server/frontend/dist/*`。访问 `/` 返回 `index.html`；未匹配路径回退到 `index.html`（SPA fallback）。API 统一 `/api` 前缀。
- **配置**：`server.embed_frontend: true` 时提供静态资源与 fallback；为 false 时仅提供 API，可单独起前端 dev server 联调。

**开发与构建**

- 在 `frontend/` 开发：`npm install`、`npm run dev`、`npm run build`（输出到 `frontend/dist`）。
- 单二进制部署：将 `frontend/dist` 复制到 `cmd/server/frontend/dist` 后 `go build ./cmd/server`，或直接使用 Docker 多阶段构建。

---

## Worker 与 rclone

- **Worker**：基于 channel 的任务队列，可配置最大并发与重试，使用 context 取消；**不直接调用 exec**，仅通过 **rclone 模块接口** 执行上传。
- **rclone 模块**：封装 `exec.Command`，支持 `--progress`，解析标准输出并返回结构化进度；项目内其它模块不得直接使用 exec。

---

## 保证与约定

- 可直接运行：`go mod tidy && go run ./cmd/server`（需正确配置与 MySQL）。
- 可直接 Docker 构建：`docker build -t rclone-sync-hub .`；`docker compose up -d` 启动 MySQL + 应用。
- 所有数据库操作经 repository 接口，service 层不依赖 Gorm；database 包封装连接与迁移，错误已包装。
- 代码含必要注释，关键逻辑未省略。
