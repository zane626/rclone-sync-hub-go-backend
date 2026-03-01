# Rclone Sync Hub（Go 后端）

文件上传调度系统：定时扫描本地目录、判断是否已上传、入队、控制并发、调用 rclone 上传、解析进度、更新数据库，并提供 REST API。生产环境可嵌入 Vue3 构建产物。

## 技术栈

- **Go 1.22+**，Gin，Gorm，**MySQL**，zap，YAML 配置
- 分层架构：api / service / repository / **database** / scheduler / worker / rclone / model / config / logger
- 接口解耦、依赖注入、无循环依赖、模块单一职责；repository 为接口，可后期切换数据库实现

## 目录结构

```
cmd/server/           # 入口：main.go，embed frontend/dist
internal/
  api/                # HTTP 层：路由与 handler，/api 前缀
  service/            # 业务编排：调用 repository、worker、scheduler
  repository/         # 数据访问接口与实现（每实体单独文件），仅此处依赖 Gorm
  database/           # 数据库连接与迁移封装（MySQL driver、连接池、AutoMigrate）
  scheduler/          # 定时扫描本地目录并入队
  worker/             # 任务队列：channel、最大并发、重试，仅通过 rclone 接口
  rclone/             # rclone 封装：唯一使用 exec 的模块，解析 --progress
  model/              # 领域模型与表结构
  config/             # 配置加载（yaml）
  logger/             # zap 封装
frontend/             # Vue3 项目（单独开发）
cmd/server/frontend/dist/  # Vue build 产出供 go:embed（Docker 多阶段会注入）
configs/              # 配置文件
migrations/           # SQL 迁移（可选，默认用 Gorm AutoMigrate）
```

## 数据库（MySQL）

- 使用 **Gorm + MySQL driver**，连接与迁移封装在 `internal/database`，**repository 保持接口**，便于后期切换数据库。
- 表结构由 **AutoMigrate** 自动迁移（启动时执行），无需手跑 SQL。
- **表**：**upload_tasks**（任务状态 pending / running / success / failed，含 progress、retry_count、error_message 等）、**file_records**（文件路径、大小、是否已上传）、**upload_logs**（进度日志）。

## 运行方式

### 本地

1. 安装 Go 1.22+、**MySQL 8.0+**、rclone。
2. 创建数据库：`CREATE DATABASE rclone_sync_hub CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;`
3. 复制 `configs/config.yaml` 并修改 `database`（host/port/user/password/dbname）、`scan.local_path`、`scan.remote_name` 等。
4. 启动（自动执行 AutoMigrate）：
   ```bash
   go mod tidy
   go run ./cmd/server
   ```
5. API 基地址：`http://localhost:8080/api`（见下）。

### 本地开发（Fresh 热重载）

适合日常开发调试：只起 MySQL，本机用 Fresh 跑 Go（兼容 Go 1.22），改代码自动重启，并默认开 Swagger。

**1. 安装 Fresh（仅需一次）**

```bash
go install github.com/zzwx/fresh@latest
```

**2. 启动 MySQL**

```bash
make dev-mysql
# 或：docker compose up mysql -d
```

**3. 启动热重载**

在项目根目录执行（使用 `configs/config.dev.yaml`：localhost MySQL + 开 Swagger + debug 日志）：

```bash
make dev
```

若不用 Make（或 Windows 下 `make dev` 未生效），可手动设环境变量再跑 fresh：

- Linux / Mac / Git Bash：`CONFIG_PATH=configs/config.dev.yaml fresh`
- PowerShell：`$env:CONFIG_PATH="configs/config.dev.yaml"; fresh` 或 `.\dev.ps1`
- CMD：`set CONFIG_PATH=configs\config.dev.yaml && fresh`

**4. 访问**

- API：http://localhost:8080/api  
- Swagger 文档：http://localhost:8080/swagger/index.html  

配置文件为项目根目录下的 **`.fresh.yaml`**，可自行调整 `main_path`、监听扩展名、排除目录等。

### Docker 构建与运行

```bash
# 构建（多阶段：先 Vue build，再 Go build，最终 alpine + rclone）
docker build -t rclone-sync-hub .

# 使用 docker-compose（含 MySQL）
# 请先将 configs/config.yaml 中 database.host 改为 mysql，并挂载 rclone 配置
docker-compose up -d
```

**Docker 说明**：  
- 多阶段：Node 构建 Vue → Go 构建二进制 → 最终镜像 alpine + 安装 rclone。  
- **docker-compose** 提供 MySQL 8.0 容器（环境变量与数据卷见 `docker-compose.yml`），应用依赖 MySQL 健康后再启动。  
- Vue 构建产物会复制到 `cmd/server/frontend/dist` 供 `go:embed` 使用。  
- 若 `frontend/` 暂无完整 Vue 项目，Dockerfile 会生成占位 `dist/index.html`，镜像仍可构建。

## Vue3 嵌入

- 使用 `go:embed` 嵌入 `cmd/server/frontend/dist/*`。
- 访问 `/` 返回 `index.html`；未匹配路径回退到 `index.html`（SPA fallback）。
- API 统一使用 `/api` 前缀，与静态资源分离。
- 配置项 `server.embed_frontend: true` 时启用静态与 fallback；为 false 时仅提供 API。

**前端开发**：  
- 在 `frontend/` 开发 Vue3，`npm run build` 输出到 `frontend/dist`。  
- 本地联调时可将 `embed_frontend` 设为 false，单独起前端 dev server 访问 API。  
- 生产或单二进制部署时：将 `frontend/dist` 复制到 `cmd/server/frontend/dist` 后执行 `go build ./cmd/server`，或直接使用 Docker 多阶段构建。

## API 示例

- `GET /api/health` — 健康检查  
- `GET /api/stats` — 各状态任务数量（pending/running/success/failed）  
- `GET /api/tasks?status=&page=1&page_size=20` — 任务列表（可选 status 筛选）  
- `GET /api/tasks/:id` — 任务详情  
- `POST /api/scan` — 触发一次目录扫描  
- `POST /api/tasks/:id/retry` — 将任务重新入队  

## Swagger 接口文档

- 使用 **swaggo/swag** 生成 OpenAPI 文档，**gin-swagger** 提供 UI；注解**仅写在 api 层**，不侵入 service。
- 访问地址：**`/swagger/index.html`**（仅当配置开启时生效）。
- 由 **config.yaml** 控制：`server.enable_swagger`。**生产环境必须设为 false**，开发环境可设为 true。

### 依赖安装与生成命令

```bash
# 安装 swag 命令行（需 Go 1.22+）
go install github.com/swaggo/swag/cmd/swag@latest

# 生成文档（解析 cmd/server/main.go 与 internal/api，输出到 cmd/server/docs）
swag init -g cmd/server/main.go -o cmd/server/docs --parseDependency --parseInternal
```

或使用 Makefile：

```bash
make install-swag   # 安装 swag
make swagger        # 安装 swag 并生成文档
make swagger-only   # 仅生成文档（要求已安装 swag）
```

### 开发与 Docker

- **本地**：修改 api 注释后执行 `make swagger-only` 或上述 `swag init`，再启动服务；在 `configs/config.yaml` 中设置 `server.enable_swagger: true` 后访问 `http://localhost:8080/swagger/index.html`。
- **Fresh 热重载**：如需改完注释自动看到文档，可在启动前执行一次 `swag init`；通常开发时改完接口跑一次 `make swagger-only` 即可。
- **Docker**：Dockerfile 中已增加「安装 swag → 执行 swag init → 再 go build」步骤，镜像内带最新文档；是否暴露 UI 仍由运行时 `server.enable_swagger` 决定，生产务必关闭。

## 配置示例（configs/config.yaml）

见仓库内 `configs/config.yaml`，包含：  
server（port、mode、embed_frontend、enable_swagger）、**database**（host、port、user、password、dbname、charset、max_open_conns、max_idle_conns、conn_max_idle_time_mins）、scan、worker、rclone、log。  
使用 docker-compose 时请将 `database.host` 设为 `mysql`。

## Worker 与 rclone

- **Worker**：基于 channel 的任务队列，可配置最大并发与重试，使用 context 取消；**不直接调用 exec**，仅通过 **rclone 模块接口** 执行上传。
- **rclone 模块**：封装 `exec.Command`，支持 `--progress`，解析标准输出并返回结构化进度；项目内其它模块不得直接使用 exec。

## 保证

- 可直接运行：`go mod tidy && go run ./cmd/server`（需正确配置与 MySQL）。  
- 可直接 Docker 构建：`docker build -t rclone-sync-hub .`；`docker-compose up -d` 启动 MySQL + 应用。  
- 所有数据库操作经 repository 接口，service 层不依赖 Gorm；database 包封装连接与迁移，错误已包装。  
- 代码含必要注释，关键逻辑未省略。
