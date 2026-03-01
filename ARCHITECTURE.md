# 项目架构说明

## 目录结构

```
rclone-sync-hub/
├── cmd/
│   └── server/
│       └── main.go              # 程序入口，依赖注入与启动
├── internal/
│   ├── api/                     # HTTP 层：仅处理请求/响应，不写业务
│   │   ├── handler.go           # 路由注册与中间件
│   │   ├── task_handler.go      # 任务相关 API
│   │   └── health_handler.go    # 健康检查
│   ├── service/                 # 业务编排层：调用 repository/worker/rclone
│   │   └── upload_service.go
│   ├── repository/              # 数据访问层：仅 Gorm 操作，每实体单独文件
│   │   ├── task_repository.go
│   │   ├── file_record_repository.go
│   │   └── upload_log_repository.go
│   ├── scheduler/               # 定时扫描本地目录，入队未上传文件
│   │   └── scanner.go
│   ├── worker/                  # 任务队列：channel、并发、重试、仅通过 rclone 接口
│   │   └── queue.go
│   ├── rclone/                  # rclone 封装：exec 仅在此模块，解析进度
│   │   └── client.go
│   ├── model/                   # 领域模型与表结构
│   │   ├── task.go
│   │   ├── file_record.go
│   │   └── upload_log.go
│   ├── config/                  # 配置加载（yaml）
│   │   └── config.go
│   └── logger/                  # zap 封装
│       └── logger.go
├── frontend/                    # Vue3 项目（单独开发，build 后 dist 被 embed）
├── configs/
│   └── config.yaml              # 默认配置
├── migrations/                  # 数据库迁移（可选，也可 Gorm AutoMigrate）
├── Dockerfile                   # 多阶段：Vue build -> Go build -> alpine + rclone
├── docker-compose.yml
└── README.md
```

## 依赖方向（禁止循环）

- `cmd` -> internal 各层
- `api` -> service
- `service` -> repository, worker, rclone, scheduler
- `worker` -> rclone, repository
- `scheduler` -> repository, worker（入队）
- `repository` -> model
- 所有层可依赖 config、logger、model

## 接口与注入

- repository / rclone / worker 对外暴露接口，便于测试与替换
- main.go 中组装具体实现并注入到 api/service
