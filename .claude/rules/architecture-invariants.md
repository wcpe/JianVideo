# 架构不变量（防架构漂移）

> 以下是本项目锁定的架构约束（依据 `docs/ARCHITECTURE.md` 与 `docs/adr/`）。**违反任一条即为架构漂移。**
> 确需改变某条 → 先写新 ADR 取代旧决策、经确认后再改；**禁止在代码里静默违背**。

## 0. v2 过渡说明

- **仓库落点（FR2-066/067）**：业务 Go 仅在 `apps/server`；生产前端在 `apps/web`；根目录无业务 `*.go` / `internal/` / 生产 `frontend/` 源码。模块路径仍为 `github.com/wcpe/JianVideo`。
- 业务模块内部分层名（api/library/transcoder/web/db）与依赖方向仍按 §1；Space、PixiJS 热区、任务队列等语义变更须 ADR。
- `docs/PRD.md`、`docs/ROADMAP.md` 与 ADR-0053/ADR-0054 描述产品目标边界，不等于允许静默违背本文件不变量。
- “禁 Redis、RabbitMQ 等外部消息队列或缓存中间件”仍然有效；v2 任务队列默认按进程内或 SQLite 持久化设计，除非新 ADR 明确批准外部中间件。
- `docs/ARCHITECTURE.md` 描述当前真貌；目录结构变更须同变更同步 ARCHITECTURE。

## 1. 模块依赖方向与分层

- 仓库落点：业务代码在 `apps/server`；`apps/web` 只通过 HTTP/API 与后端交互，禁止 app-to-app 依赖（`apps/web` ↛ `apps/wiki`）。
- `apps/server/internal` 依赖方向严格单向：`web` → `api` → `library` / `playback` / `player` / `subtitle` / `transcoder` → `db`，禁止反向依赖。
- **数据访问分层（FR2-070 / ADR-0058）**：领域 CRUD/查询经各包 `repository`（接口 + GORM 实现）；`api` 生产代码禁止对业务表直接拼 `*gorm.DB` 读写。允许：经 service 委托、对领域错误（含 `gorm.ErrRecordNotFound`）做 HTTP 映射、同事务跨域钩子经 `settings.TxRepository`（实现 `tasks.Tx` / `library.DBProvider`）直接传入，无需在 api 写 `tx.DB()`。任务入队对外签名为 `tasks.EnqueueTx(ctx, tasks.Tx, …)`（domain 可用 `tasks.AsTx(*gorm.DB)`）；推断 space 列表为 `library.ListSpacesNeedingInferenceBackfill(ctx, library.DBProvider, …)`。library 已下沉：MediaQuery（含写路径全切片、summary/dedup/file_hash、**UpdateFileStateCAS/GetByIDAndDeletedAtTx** 等 recycle CAS）/ libraryPath / mediaTypeRule / space / chapter / bookmark / metadata / tag / album / cover / view / watch / inference / health；**library 生产路径无 `s.db`**（含 service/summary/next_episode/dedup/file_hash/media_type_rules/bookmark/watch/recycle_cleanup）。测试文件可直连 db 造数。禁止引入 sqlx 等第二套查询栈。
- `db` 模块不依赖任何业务模块，仅提供纯数据读写能力。
- `watcher` 模块仅向 `library` 报告文件事件，不直接操作数据库。
- `web` 模块不直接调用 `transcoder`，通过 `library` / `api` 间接协调。
- 禁止在 `db` 模块中编写业务逻辑（如转码决策、认证校验）。
- 前端共享能力放 `packages/*`；`packages/*` 不得反向依赖 `apps/*`。

## 2. 简单优先（禁用的重型件）

- 禁止引入 Redis、RabbitMQ 等外部消息队列或缓存中间件。
- 使用 GORM 作为唯一 ORM，禁止引入其他 ORM 框架（如 XORM、Ent）。
- 禁止引入微服务框架、服务发现、容器编排等分布式组件。
- 当前阶段禁止引入 gRPC，仅使用 HTTP RESTful API。
- 如确需引入上述组件，必须先写 ADR 并经确认。

## 3. 真源与一致性约束

- 媒体库目录注册信息以 SQLite `library_paths` 表为唯一真源。
- 媒体文件元数据以 SQLite `media_files` 表为唯一真源，禁止以文件系统状态覆盖数据库记录。
- 转码 / 播放会话状态以 `playback.Service` 的内存会话（key=media_id）为单一真源、不落持久表（见 [ADR-0036](../../docs/adr/0036-codec-negotiation.md)）；FFmpeg 进程由 transcoder 在进程内管理，进程重启会话即丢失。
- 前端静态资源以 `apps/web` 构建产物为准，经同步后由 `apps/server` 的 `//go:embed all:web/dist` 内嵌为运行时真源。
- 版本号以根目录 `VERSION` 文件为唯一真源。
- **HTTP API 契约（FR2-071）**：机器可读真源为 `api/openapi.yaml`（设计优先）。变更契约后须：`openapi:check`（结构 + media-client/mock 生产源 `/api/v2` 路径对齐）→ `cd apps/server && task gen` 重生成 `internal/openapi/api.gen.go` → `task gen:check` 防漂移。生成物不得手改。CLI 用 `go run oapi-codegen@v2.8.0`（不入 go.mod）；运行时依赖 `oapi-codegen/runtime`。运行时桥接：`/health`（openapi.GetHealth）、auth 契约类型响应、`/api/v2/media|tasks` 薄适配单挂；**禁止**现网一步 `RegisterHandlers` 全量挂载。TS 侧本波为路径防漂移（无新依赖、无 codegen）；人类可读补充见 `docs/API.md`，不得与 openapi 已覆盖路径矛盾。

## 4. 技术栈锁定

- 后端语言：Go（1.22+），禁止引入其他语言编写的后端服务。
- 前端框架：React + TypeScript，通过 Vite 构建。
- 数据库：SQLite WAL 模式，使用 `mattn/go-sqlite3` 或 `modernc.org/sqlite` 驱动。
- 视频播放内核：mpegts.js，禁止替换为原生 `<video>` 标签播放 TS 流。
- 转码引擎：FFmpeg，作为外部进程调用，禁止内嵌编解码逻辑。

## 红线（出现即停止并先确认）

- 在仓库根恢复业务 `*.go` / `internal/` / 生产前端源码树（须保持 `apps/server` + `apps/web`）。
- 引入被禁的重型件（Redis、额外 ORM、微服务框架等）。
- 破坏模块依赖方向（反向依赖、跨层调用）。
- 破坏真源一致性（多处记录同一事实且互相矛盾）。
- 擅自更换技术栈（后端语言、前端框架、数据库、播放内核）。
- 静默违背任一已接受 ADR。
