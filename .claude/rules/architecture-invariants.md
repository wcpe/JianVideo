# 架构不变量（防架构漂移）

> 以下是本项目锁定的架构约束（依据 `docs/ARCHITECTURE.md` 与 `docs/adr/`）。**违反任一条即为架构漂移。**
> 确需改变某条 → 先写新 ADR 取代旧决策、经确认后再改；**禁止在代码里静默违背**。

## 1. 模块依赖方向与分层

- 依赖方向严格单向：`web` → `media-library` / `transcoder` → `db`，禁止反向依赖。
- `db` 模块不依赖任何业务模块，仅提供纯数据读写能力。
- `watcher` 模块仅向 `media-library` 报告文件事件，不直接操作数据库。
- `web` 模块不直接调用 `transcoder`，通过 `media-library` 间接协调。
- 禁止在 `db` 模块中编写业务逻辑（如转码决策、认证校验）。

## 2. 简单优先（禁用的重型件）

- 禁止引入 Redis、RabbitMQ 等外部消息队列或缓存中间件。
- 禁止引入 ORM 框架，数据库操作使用标准 `database/sql` + 参数化查询。
- 禁止引入微服务框架、服务发现、容器编排等分布式组件。
- 当前阶段禁止引入 gRPC，仅使用 HTTP RESTful API。
- 如确需引入上述组件，必须先写 ADR 并经确认。

## 3. 真源与一致性约束

- 媒体库目录注册信息以 SQLite `library_paths` 表为唯一真源。
- 媒体文件元数据以 SQLite `media_files` 表为唯一真源，禁止以文件系统状态覆盖数据库记录。
- 转码会话状态以 `transcode_sessions` 表为准，FFmpeg 进程状态须与数据库状态一致。
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
- 在服务层（business logic）中直接调用 Bukkit/Spigot API（本条不适用，保留格式）。
