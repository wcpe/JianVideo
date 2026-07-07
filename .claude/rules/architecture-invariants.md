# 架构不变量（防架构漂移）

> 以下是本项目锁定的架构约束（依据 `docs/ARCHITECTURE.md` 与 `docs/adr/`）。**违反任一条即为架构漂移。**
> 确需改变某条 → 先写新 ADR 取代旧决策、经确认后再改；**禁止在代码里静默违背**。

## 0. v2 过渡说明

- 本文件约束当前代码真貌；在 apps/packages 迁移、Space、PixiJS 热区和 v2 任务队列真正落地前，旧分层、旧模块名和旧技术栈锁定仍然有效。
- `docs/PRD.md`、`docs/ROADMAP.md` 与 ADR-0053/ADR-0054 描述的是 v2 目标边界，不等于允许代码静默违背当前不变量。
- P0.5 或后续阶段如需改变模块依赖方向、前端包边界、任务队列语义、Space 真源或 PixiJS 渲染边界，必须在同一变更中写新的取代 ADR，并同步更新本文件。
- “禁 Redis、RabbitMQ 等外部消息队列或缓存中间件”仍然有效；v2 任务队列默认按进程内或 SQLite 持久化设计，除非新 ADR 明确批准外部中间件。
- `docs/ARCHITECTURE.md` 仍只描述当前系统真貌；目标结构先放 PRD、ROADMAP、ADR 和 specs，代码迁移完成后再同步 ARCHITECTURE。

## 1. 模块依赖方向与分层

- 依赖方向严格单向：`web` → `media-library` / `transcoder` → `db`，禁止反向依赖。
- `db` 模块不依赖任何业务模块，仅提供纯数据读写能力。
- `watcher` 模块仅向 `media-library` 报告文件事件，不直接操作数据库。
- `web` 模块不直接调用 `transcoder`，通过 `media-library` 间接协调。
- 禁止在 `db` 模块中编写业务逻辑（如转码决策、认证校验）。

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
- 前端静态资源以 `frontend/dist/` 编译产物为准，`go:embed` 内嵌后即为运行时真源。
- 版本号以根目录 `VERSION` 文件为唯一真源。

## 4. 技术栈锁定

- 后端语言：Go（1.22+），禁止引入其他语言编写的后端服务。
- 前端框架：React + TypeScript，通过 Vite 构建。
- 数据库：SQLite WAL 模式，使用 `mattn/go-sqlite3` 或 `modernc.org/sqlite` 驱动。
- 视频播放内核：mpegts.js，禁止替换为原生 `<video>` 标签播放 TS 流。
- 转码引擎：FFmpeg，作为外部进程调用，禁止内嵌编解码逻辑。

## 红线（出现即停止并先确认）

- 引入被禁的重型件（Redis、ORM、微服务框架等）。
- 破坏模块依赖方向（反向依赖、跨层调用）。
- 破坏真源一致性（多处记录同一事实且互相矛盾）。
- 擅自更换技术栈（后端语言、前端框架、数据库、播放内核）。
- 静默违背任一已接受 ADR。
