# 架构说明

## 总体结构

```text
Browser / API client
        │ HTTPS / Bearer
        ▼
Reverse proxy ───────────────► /metrics (private Prometheus)
        │
        ▼
Gin API ─► service ─► repository ─► MySQL
   │          │                         ▲
   │          ├─► local scanner ────────┤
   │          ├─► remote route scanner ─┤
   │          └─► worker ─► rclone ─────┤
   └─► in-process SSE hub
```

MySQL 不只是业务数据存储，也是扫描和上传执行的协调中心。应用进程可以重启或横向扩容；正确性不依赖进程内 channel。进程内 channel 只用于低延迟唤醒和页面事件，不承担持久化。

## 包职责与依赖方向

```text
cmd/server      组装依赖、生命周期、HTTP 与优雅退出
internal/api    HTTP 适配、鉴权、限流、审计、安全响应
internal/service 业务用例与输入校验
internal/scheduler 目录扫描、文件版本识别、任务生成
internal/worker MySQL 任务租约、重试、取消、rclone 编排
internal/rclone 唯一允许启动 rclone 子进程的包
internal/repository 数据访问与事务边界
internal/database 连接池和版本化迁移
internal/security 凭据、令牌和资源白名单
internal/observability 指标
internal/events 进程内 SSE 快照分发
internal/model 持久化模型和状态常量
```

依赖从 API/后台执行层指向 service/repository；repository 之外不写 GORM 查询，rclone 包之外不执行外部命令。

## 扫描流程

定时扫描与手工扫描走同一条持久化调度链路。手工接口只把启用目录的 `next_scan_at` 更新为当前时间并发送进程内唤醒信号，然后返回 `202`；即使信号丢失，数据库中的到期状态也会被后续轮询发现。

```text
轮询到期目录
  → 用数据库条件更新原子领取扫描租约
  → 定时续约；超时、失租或关机即取消
  → 一次性读取该目录文件快照和未完成任务
  → WalkDir 顺序遍历本地目录
  → size + mtime(ns) 生成版本指纹
  → 跳过仍在稳定窗口内的文件
  → 批量创建新快照、事务分批更新变化快照
  → 通过唯一 idempotency_key 批量创建任务
  → 标记完整扫描确认已消失的文件
  → 写 scan_runs 和下一次扫描时间
  → 释放扫描租约
```

重要约束：

- 扫描周期由 `next_scan_at` 驱动；`error` 可重新领取，不会形成永久停止状态。
- 同一应用内扫描周期由互斥锁防重；多个实例由目录租约防重。
- 不同目录可小规模并行，同一目录顺序遍历。批处理降低数据库往返，但不会无界增加磁盘并发。
- 监听目录之间禁止父子路径重叠，避免同一绝对文件路径被两个目标同时认领。
- 指纹包含目标 remote/path，因此目标变化会生成新任务。
- 文件在稳定窗口内不建单；Worker 在上传前后都检查指纹，上传过程中发生变化的版本不会被标记为成功。
- 高频本地扫描不访问远端 API。远端发现由独立、低频的路由扫描器执行，不会重新把逐文件远端调用引入本地扫描热路径。

## 远端路由扫描流程

远端目标从监控目录中抽离为可复用路由。每个监控目录保存 `remote_route_id`，同时保留派生后的 remote/path 快照，保证上传热路径无需额外联表。历史监控目录在迁移时按 remote/path 自动归并并绑定路由。

```text
轮询到期远端路由
  → 原子领取持久化扫描租约并定时续约
  → rclone lsjson --recursive 流式读取对象
  → 标准化路径并补齐祖先目录节点
  → 按批 upsert 文件与目录索引
  → 仅在完整扫描成功后标记未出现记录为 missing
  → 保存文件数、容量、耗时、错误和下一扫描时间
  → 释放租约
```

修改路由目标会在事务中锁定关联监控目录；活动本地扫描期间拒绝变更。成功变更后，旧目标任务被取消或请求取消、已上传标记失效，并立即安排关联目录重新扫描，避免旧目标结果污染新路由状态。

## 上传任务状态机

```text
                 pause
pending ─────────────────────► paused
   │                             │ retry
   │ atomic claim + lease        ▼
   ├──────────────────────────► pending
   ▼
running ── success ───────────► success
   │
   ├─ transient failure ──────► pending (next_retry_at + backoff)
   ├─ exhausted/invalid ──────► failed
   ├─ cancel request ─────────► canceled
   └─ process shutdown ───────► pending (lease release or expiry recovery)
```

领取使用事务和 `FOR UPDATE SKIP LOCKED`。`lease_owner`、`lease_expires_at` 和心跳保证多个 Worker 不会正常执行同一任务；异常退出后，过期租约可以被重新领取。重试次数、下次时间和取消请求都在 MySQL 中持久化。

成功提交是一个数据库事务：

1. 只有仍由当前 owner 持有的 running 任务才能转为 success。
2. 只有文件快照指纹、remote 名称和 remote 路径仍与任务创建时完全一致，才设置 `uploaded_at`。
3. 两个更新一起提交或一起回滚。

如果上传期间文件变更，Worker 会把旧任务终结为 `canceled`，不会写入该文件版本的 `uploaded_at`；下一轮扫描会为新指纹生成不同幂等键的任务。

## 数据模型

| 表 | 用途 |
|---|---|
| `watch_folders` | 目录配置、远端路由绑定、调度时间、扫描租约、最近状态和累计统计 |
| `file_records` | 本地路径快照、版本指纹、目标、上传时间、缺失时间 |
| `remote_routes` | 可复用 rclone 目标、后台扫描周期、租约、状态和索引统计 |
| `remote_file_records` | 远端文件/目录路径索引、大小、修改时间、最近发现和缺失时间 |
| `upload_tasks` | 持久化队列、幂等键、状态、租约、重试和取消信息 |
| `upload_logs` | 节流后的上传进度和结果日志 |
| `scan_runs` | 每轮目录扫描的耗时、文件数、跳过/缺失/错误统计 |
| `audit_logs` | HTTP 变更操作的主体、角色、请求 ID、路径和状态码 |
| `schema_migrations` | 迁移版本、名称、dirty 状态和应用时间 |

迁移列表只能追加。应用启动时通过 MySQL advisory lock 串行执行迁移；失败版本保持 dirty，要求人工检查，防止多个实例继续在未知 schema 上运行。

维护循环按小批次清理上传日志、终态任务、扫描历史和审计日志；待处理、运行中与暂停任务不受任务保留策略影响。

## 安全边界

- 生产镜像默认开启认证；缺少强密码、签名密钥、本地根目录，或 `rclone.conf` 中没有可用 remote 时拒绝启动。remote 默认自动发现，也可显式收窄为子集。
- admin 可变更任务和目录，viewer 只读。
- 本地路径先转绝对路径并解析符号链接，再检查是否位于允许根目录。
- remote 名称必须在允许列表，远端路径标准化并拒绝穿越。
- rclone 配置只使用 `listremotes --long`，不会通过 `config show` 把凭据读进应用输出。
- 只有显式配置的代理 CIDR才可信；默认忽略伪造的转发客户端 IP。
- 生产容器以 UID 10001 运行，根文件系统和源数据只读，移除 Linux capabilities，并设置进程/内存/CPU/日志限额。独立 rclone 配置卷可写，以支持 OAuth token 持久化刷新。

## 一致性与高可用

可以运行多个应用实例，但必须满足：

- 共享同一个 MySQL，并且所有实例看到相同的本地目录路径和 rclone 配置。
- 数据库连接和 MySQL 会话统一使用 UTC；所有应用主机与数据库仍必须通过 NTP 保持时钟同步，因为任务和扫描租约依赖持久化的绝对时间。
- SSE hub 是进程内组件，不用于业务一致性。浏览器会定期重新读取数据库状态。
- schema migration 由 advisory lock 串行化；应用发布仍应采用向后兼容的 expand/migrate/contract 流程。
- Compose 中的单 MySQL 适合单机部署，不等于数据库高可用。

## 故障恢复语义

| 故障 | 恢复行为 |
|---|---|
| 目录暂时卸载/无权限 | 本轮记录 failed，计算下一扫描时间，后续自动重试 |
| 扫描进程崩溃 | 心跳停止，租约约 90 秒后过期，其他实例接管 |
| 远端列举中断/超时 | 路由记录 error 并自动重试；不把未完成结果误判为远端缺失 |
| rclone/网络失败 | 持久化指数退避；达到上限后 failed |
| Worker 崩溃 | 任务租约过期后重新领取，不依赖内存队列 |
| 文件上传中发生变化 | 旧任务结果不标记新指纹；下一轮为新版本建单 |
| 数据库暂时不可用 | readiness 失败；后台操作保留状态并在轮询/租约语义下恢复 |
| 优雅关机 | 停止接流量，取消扫描/上传，释放任务租约，等待后台服务退出 |

## 可观测性

- 所有 HTTP 响应带 `X-Request-ID`。
- `/api/health/live` 只检查进程，`/api/health/ready` 检查数据库。
- Prometheus 指标覆盖 HTTP 延迟/状态、真实数据库 readiness、扫描结果/耗时、到期未扫描目录、Worker 结果/重试、待处理队列年龄和数据库连接池。
- 扫描历史和审计日志提供业务级追踪；JSON 日志提供进程级诊断。
- 指标标签只使用有限集合，不把文件路径、任务 ID 或用户输入作为标签，避免高基数。

## 部署产物

Dockerfile 分为前端、Go 和运行时三个阶段。构建错误不会被忽略；运行层只包含静态 Go 二进制、rclone、CA 与时区数据。发布流水线先执行格式、测试、race、vet、依赖漏洞、前端审计和 MySQL 集成测试，再生成多架构镜像、SBOM 和 provenance。
