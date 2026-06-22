# 功能规格：扫描任务队列 + 页眉任务展示

> 状态：开发中　·　关联 PRD：FR-29　·　分支：feature/fr-29-taskqueue

## 1. 背景与目标

当前扫描经 `POST /api/library/scan/:id` 直接 `StartAsyncScan` 在后台 goroutine 执行，全局只跟踪单一 `ScanStatus`（同一时刻仅一个扫描任务）。多次触发扫描会互相覆盖进度、并发抢资源，且没有任务历史，用户看不到「有哪些扫描在排队 / 进行中 / 已完成」。

本功能（P2）引入**扫描任务队列**：多次触发扫描排队、由单 worker 串行执行；页眉常驻展示当前进行中的任务，点开看任务列表与各自进度。队列经 SQLite 持久化，服务重启后把残留 `running` 任务重置为 `pending` 重新入队，避免重启丢任务。

本功能只做**触发 / 队列 / 页眉**层，**不重写扫描执行逻辑**（增量/全量对账是 FR-27）。worker 调用现有 `ScanLibraryWithType(libraryID, path, type)` 同步执行；FR-27 后续扩展其 mode，本期按现有签名调用，集成时再对接。

## 2. 需求（要什么）

- 范围内：
  - 新增 `ScanTask` 模型 / 表（`library_id`、`scan_type`(full/incremental)、`status`(pending/running/completed/error)、`scanned_files`、`total_files`、`error`、`created_at`、`started_at`、`completed_at`），加入 `main.go` 的 `AutoMigrate` 列表。
  - 扫描**入队**：触发扫描（`POST /api/library/scan/:id`）改为建 `pending` 任务入队，立即返回；单 worker goroutine 串行从队列取最早 `pending` 执行（调 `ScanLibraryWithType`）。
  - **重启恢复**：服务启动时把残留 `running` 任务重置为 `pending` 重新入队。
  - 端点：`GET /api/library/scan/tasks` 列任务（含当前进行中任务的实时进度）。
  - 前端 `AppLayout` 页眉常驻展示进行中任务指示（有进行中任务时显示），点开 Popover 展示任务列表与各自进度。
- 不做（范围外）：
  - 增量 / 全量扫描的执行逻辑与已删文件对账（FR-27）。`scan_type` 字段先落库、worker 暂统一按现有 `ScanLibraryWithType` 全量行为调用，full/incremental 的差异留 FR-27 对接。
  - 定时扫描调度（FR-28）。
  - 任务取消 / 删除 / 重试端点（本期不做，避免镀金）。
  - 多 worker 并行扫描（明确要求单 worker 串行）。

## 3. 设计（怎么做）

- 数据模型：`internal/db/models/scan_task.go` 新增 `ScanTask`，主键自增 ID，按上述字段；`status` 以常量集中定义（pending/running/completed/error），`scan_type` 常量（full/incremental），避免魔法字符串。
- 队列与 worker：`internal/library/task_queue.go` 新增 `TaskQueue`，持有 `*gorm.DB` 与执行函数（注入 `Service.ScanLibraryWithType`，便于测试替身），不在 `db` 模块写业务逻辑（依赖方向 library→db 不变）。
  - 入队 `Enqueue(libraryID, path, dirType, scanType)`：建 `pending` 任务落库，唤醒 worker。
  - worker `loop`：单 goroutine，取最早 `pending`（`ORDER BY created_at, id`），置 `running`（记 `started_at`）→ 调执行函数 → 按返回置 `completed`（记 `scanned_files`、`completed_at`）或 `error`（记 `error`、`completed_at`）；队列空时阻塞等待（条件变量 / channel 信号），不空转轮询。
  - `RecoverRunning()`：启动时把残留 `running` 改回 `pending`（重启恢复），随后启动 worker 自然重新执行。
  - 临界区只保护队列信号与状态读写，扫描执行（高开销 IO）在锁外。
- 进度桥接：worker 串行执行，全局 `ScanStatus`（`scan_status.go`）始终对应当前 `running` 任务。`ListScanTasks` 把实时 `GetScanStatus()` 的 `scanned_files`/`total_files` 覆盖到 `running` 任务上返回（已完成任务用其持久化的 `scanned_files`）。不在 worker 内每 tick 写库，减少写放大。
- API：`internal/api/handler.go` 的 `ScanLibrary` 改为 `queue.Enqueue(...)` 并返回 `{"status":"queued","task_id":N}`；新增 `ListScanTasks`（`GET /api/library/scan/tasks`，返回 `{"tasks":[...],"current":task|null}`）。Handler 经 `WithScanQueue` 注入队列（沿用既有链式注入风格），未注入时 `ScanLibrary` 回退原 `StartAsyncScan`、`tasks` 返回空，保持测试与无队列环境可用。
- 装配：`main.go` 建 `TaskQueue`、先 `RecoverRunning()` 再 `Start()`，注入 Handler；`web.NewRouter` 路由新增 `GET /api/library/scan/tasks`。
- 前端：`api/library.ts` 加 `getScanTasks()`（real+mock）与 `ScanTask` 类型；`AppLayout` 头部加任务指示器组件（轮询 `getScanTasks`，有进行中任务时显示数量徽标 + 进度，Popover 展开任务列表）；MSW handler 加 `GET /api/library/scan/tasks`。

## 4. 任务拆分

- [ ] 后端 `ScanTask` 模型 + 加入 AutoMigrate
- [ ] 后端 `TaskQueue`（入队 / 单 worker 串行 / 重启恢复）+ 单测（含 -race）：入队→执行→状态流转、串行性、重启 running→pending 恢复
- [ ] 后端 API：`ScanLibrary` 改入队 + `ListScanTasks` + 路由 + 测试
- [ ] main.go / web.NewRouter 装配队列
- [ ] 前端 `ScanTask` 类型 + `getScanTasks`（real+mock）+ MSW handler
- [ ] 前端 `AppLayout` 页眉任务指示器组件 + 测试
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准

- 多次触发扫描后，任务按入队顺序排队、由单 worker **串行**执行（任意时刻至多一个 `running`）；`GET /api/library/scan/tasks` 能查到各任务的状态与进度。
- 入队→`running`→`completed` 状态流转正确，`completed` 任务持久化 `scanned_files`；执行出错置 `error` 并记 `error` 信息。
- 重启恢复：服务启动调 `RecoverRunning()` 后，残留 `running` 任务被重置为 `pending` 并重新执行（集成测试模拟：预置一条 `running` 记录 → 新建队列 → 恢复 → 该任务最终 `completed`）。
- 前端页眉在有进行中任务时显示指示器，点开展示任务列表与各自进度（经 mock/MSW 验证）。
- 后端 `go build ./...` 通过，受影响包 `go test` 全绿，队列 / worker 并发 `go test -race` 无竞争；前端 `npm run build` 与 `npm run test` 全绿。

## 6. 风险 / 待定

- `scan_type` 与 FR-27 的对接：本期字段先落库、worker 统一全量调用，full/incremental 的执行差异留 FR-27；本期不为其预留多余抽象（避免镀金）。
- 进度桥接依赖「单 worker 串行 ⇒ 全局 ScanStatus 对应当前 running 任务」这一不变量；若未来改多 worker（范围外）需改为每任务独立进度，故本期保持单 worker。
