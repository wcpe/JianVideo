# 功能规格：通用异步任务队列中心

> 状态：已交付@v0.23.0　·　关联 PRD：FR2-037　·　阶段：P2 `0.23.x`　·　分支：codex/fr2-037-task-center

## 1. 背景与目标

当前已有扫描 `scan_tasks` 与转码 `transcode_tasks` 两套局部队列，状态、重试、恢复、监控和 API 各自实现。ADR-0055 已决定 P2 建立 SQLite 持久化 + 进程内 worker 池的通用任务队列中心，并要求状态统一为 `pending` / `running` / `succeeded` / `failed` / `canceled`。P2 后续的转码、缩略图、缓存清理、工具下载、元数据回填、去重、智能封面都应复用它。

目标：

- 建立统一 `tasks` 真源、状态机、调度器、worker pool 与 API。
- 收敛旧扫描/转码任务状态映射，避免后续继续扩散局部队列。
- 提供任务列表、详情、统计、取消、重试和进度更新能力。
- 任务全程 Space scoped、可审计、可恢复、可 Benchmark。

## 2. 需求（要什么）

- 新增统一任务模型：
  - 类型、状态、优先级、重试次数、最大重试、进度、断点、幂等键、payload、错误摘要、scope、Space、关联资源、时间戳。
  - 媒体/库相关任务使用 `scope=space` 且 `space_id` 非空；工具下载、硬件重测等全局操作使用 `scope=system`，其 `space_id` 为空或固定系统归属，最终口径以 ADR-0063 审核结果为准。
- 状态机：
  - `pending -> running -> succeeded/failed/canceled`
  - `failed` 在可重试时按退避回到 `pending`
  - `running` 在进程重启后按类型策略恢复为 `pending` 或 `failed`
- 调度：
  - 按任务类型分 worker pool。
  - 按优先级与入队时间领取任务，并实现公平策略，避免持续高优先级任务让低优先级任务永久饥饿。
  - 公平策略可采用 aging、配额轮转或连续高优先级领取上限，但必须有可重复测试证明低优先级任务最终会被领取。
  - 同一幂等键未完成任务不得重复入队。
  - Web 请求只入队并返回任务 ID，不在请求线程执行重任务。
- API：
  - 任务列表、详情、统计、取消、重试。
  - 支持按 Space、类型、状态、关联资源过滤。
  - 旧 mock/client 任务状态契约对齐 FR2-006。
- 旧队列兼容：
  - 扫描与转码旧状态 `completed/error` 映射为 `succeeded/failed`。
  - 迁移或适配旧 API，避免现有页面立即失效。
- 范围内：任务表、repository、调度器、worker 注册接口、基础 API、旧状态兼容、单机恢复。
- 不做（范围外）：外部消息队列、分布式 worker、多实例抢占、完整 AI 任务实现、每个业务任务的最终业务逻辑。

## 3. 设计（怎么做）

Schema：

- `tasks`：`id`、`scope`、`space_id`、`type`、`status`、`priority`、`attempts`、`max_attempts`、`progress`、`checkpoint`、`idempotency_key`、`payload_json`、`resource_type`、`resource_id`、`error`、`created_at`、`updated_at`、`started_at`、`finished_at`。
- 索引：
  - `(space_id, status, priority, created_at, id)`
  - `(type, status, priority, created_at, id)`
  - `(space_id, type, status, updated_at)`
  - `idempotency_key` 对未完成任务做唯一约束或应用层保护。

模块：

- 新增 `internal/tasks`，只依赖 `db`/repository，不反向依赖业务模块。
- 业务模块通过 worker registry 注册处理器，例如 `transcode.hls`、`library.scan`、`thumbnail.generate`。
- 调度器只负责领取、状态推进、重试、取消信号；业务处理器负责执行 payload。

恢复：

- 启动时扫描 `running` 任务，按处理器声明的恢复策略处理。
- 默认策略为重新排队到 `pending`，高危不可幂等任务必须显式声明失败或断点续跑。

API：

- `GET /api/tasks`
- `GET /api/tasks/:id`
- `GET /api/tasks/stats`
- `POST /api/tasks/:id/cancel`
- `POST /api/tasks/:id/retry`

审计：

- 任务创建、取消、重试、失败终态写 FR2-040 审计事件。

## 4. 任务拆分

- [x] 新增统一 `tasks` model、repository 与状态机单元测试。
- [x] 实现入队、幂等键、领取、进度更新、成功/失败/取消、重试退避。
- [x] 实现 worker pool registry、按类型并发上限与公平调度策略。
- [x] 实现启动恢复 `running` 任务。
- [x] 新增任务 API 与 Space scoped 过滤。
- [x] 兼容旧扫描/转码任务状态，保留旧 API 适配层。
- [x] 接入任务审计事件接口。
- [x] 补单元测试：状态机、优先级、公平调度、幂等、重试、取消、恢复。
- [x] 补集成测试：SQLite 并发入队/领取、重启恢复、旧状态映射。
- [x] 补 E2E：任务中心列表、过滤、取消、重试。
- [x] 补 Benchmark：大任务表下列表/统计/领取延迟。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 新任务状态只使用 `pending/running/succeeded/failed/canceled`；旧 `completed/error` 在边界层被兼容映射。
- 同一幂等键未完成任务重复入队只返回既有任务，不重复执行。
- 并发重复使用同一 `idempotency_key` 入队时，只产生一个未完成任务。
- 持续有高优先级任务入队时，低优先级任务不会永久饥饿，公平策略有单元测试证明。
- 任务取消、失败重试、进度更新和重启恢复都有自动化测试覆盖。
- 任务列表/统计可按 Space、类型、状态过滤，跨 Space 不泄露。
- 系统级任务与 Space 级任务可明确区分；Space scoped 查询默认不返回 `scope=system` 任务，除非用户有系统管理上下文。
- 任务查询 Benchmark 达到 FR2-003 §4 的任务队列查询门槛，报告进入 `.tmp/`。
- 扫描/转码旧页面在适配层下不崩溃；新任务 API 可被 `packages/media-client` 消费。
- `go test`、任务集成测试、Playwright 任务中心 E2E、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，入队、查看进度、取消、重试流程实跑通过。

## 6. 风险 / 待定

- 已确认：旧 `scan_tasks` / `transcode_tasks` 迁移进 `tasks`，旧表只做一次性迁移与兼容查询。
- 已确认：worker 并发上限首批默认值为转码 1、缩略图 4、扫描 1、轻任务 2，并通过 FR2-024 registry 配置。
- 已确认：系统级任务的 Space 归属采用 ADR-0063 的 `scope=system + space_id NULL`。
- 如果任务处理器不可幂等，必须在对应业务 FR 的 spec 中写明断点与恢复策略。
