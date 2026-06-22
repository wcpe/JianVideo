# 功能规格：定时扫描

> 状态：开发中　·　关联 PRD：FR-28　·　依赖：FR-29（扫描任务队列）、FR-24（设置子源）

## 1. 背景与目标

实时文件监听（fsnotify）覆盖本地目录的即时变更，但存在盲区：网络目录、监听器丢事件、服务未运行期间发生的增删。本功能（P2）补一道**周期性兜底扫描**：按设置中可配置的周期，后台定时对所有启用的媒体库入队**增量**扫描，捕获被实时监听漏掉的变化。

周期以 SQLite `settings` 表的 `scan_interval`（秒）为唯一真源（FR-24 已建该键并由设置页可改）。本功能只做**后台调度**，不重写扫描执行（FR-27）、不重写入队（FR-29）：到点经 FR-29 队列入队增量扫描即可。

## 2. 需求（要什么）

- 范围内：
  - 后台定时调度器：按 `settings.scan_interval`（秒）周期触发；周期 `<=0` 或非法 → 关闭定时扫描（不触发）。
  - 到点对**所有启用**的媒体库（`enabled=1`，本地与 SMB 同等对待）入队**增量**扫描（`scan_type=incremental`），经 FR-29 队列串行执行。
  - **防积压**：入队前跳过该库已有「活动任务」（`pending`/`running`）的情况，避免周期短于扫描耗时时任务无限堆积。
  - **周期热生效**：设置页改 `scan_interval` 保存后，调度器即时按新周期重排，无需重启（重启同样按设置生效）。
- 不做（范围外）：
  - 启动即扫一次：调度器**等满一个周期**才首次触发（避免每次重启都触发扫描风暴；启动期由实时监听 / 手动扫描覆盖）。
  - 扫描周期的最小值 / 上限校验（交由设置页与运维约定，调度器忠实于配置值）。
  - 全量对账型定时扫描（定时只做增量；全量对账经手动 `mode=full` 触发，见 FR-27）。
  - 按库单独配置周期、定时任务历史的额外展示（FR-29 任务列表已含定时入队的任务）。

## 3. 设计（怎么做）

- 调度器：`internal/library/scheduler.go` 新增 `ScanScheduler`，**纯定时组件**，经注入解耦、便于 -race 测试：
  - `intervalFn func() time.Duration`：返回当前周期（`<=0` 表示关闭）。装配时读 `settings.scan_interval` 解析为秒。
  - `triggerFn func()`：触发一轮全库入队。装配时枚举启用库经队列入队增量扫描。
  - `Start()`（幂等启动单 goroutine 循环）、`Stop()`（幂等）、`Reload()`（周期变更后非阻塞唤醒重排）。
  - 循环：每轮先读 `intervalFn`；`<=0` 则阻塞等 `Reload`/`Stop`；否则 `time.NewTimer(interval)`，到点调 `triggerFn` 进入下一轮，期间收到 `Reload` 则重算、收到 `Stop` 则退出。临界区只护生命周期标志，触发（高开销）在锁外。
- 全库入队：`TaskQueue` 新增 `EnqueueScheduled(libs []models.LibraryPath, scanType string) int`——逐库跳过 `enabled!=1` 与已有活动任务者，入队并返回入队数量；`hasActiveTask(libraryID)` 查 `status IN (pending,running)`。复用 FR-29 既有 `Enqueue`，不另写扫描。
- 热生效：`api.Handler` 加可选 `WithSettingsReload(fn func())`，`UpdateSettings` 成功保存后调用（nil 安全）；装配时传 `scheduler.Reload`。设置 api 不直接依赖调度器具体类型，只持一个回调。
- 装配：`main.go` 建 `ScanScheduler`（`intervalFn` 读设置、`triggerFn` 枚举启用库经 `scanQueue.EnqueueScheduled` 入队增量），`Start()` 并 `defer Stop()`；把 `scheduler.Reload` 注入 Handler。

## 4. 任务拆分

- [ ] 后端 `ScanScheduler`（定时 / 关闭 / 热生效 / 停止）+ 单测（含 -race）
- [ ] 后端 `TaskQueue.EnqueueScheduled` + `hasActiveTask` + 单测（启用过滤、活动任务跳过、入队计数）
- [ ] 后端 `api.Handler.WithSettingsReload` + `UpdateSettings` 调用 + 测试
- [ ] main.go 装配调度器并注入热生效回调
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准

- 周期 `>0` 时调度器按周期触发，多周期内多次触发（注入小周期单测：计数随时间增长）；周期 `<=0` 时不触发。
- 触发一轮对所有启用库入队增量扫描，跳过已有 `pending`/`running` 任务的库；入队计数正确（含真实内存库的 `EnqueueScheduled` 测试）。
- 改 `scan_interval` 保存后 `Reload` 即时按新周期重排（单测：初始关闭→改小周期+Reload→开始触发）。
- `Stop` 后不再触发；`Start` 幂等（重复调用仅一个循环）。
- 后端 `go build ./...` 通过，受影响包 `go test` 全绿，调度器并发 `go test -race` 无竞争。

## 6. 风险 / 待定

- 周期短于扫描耗时：靠「活动任务跳过」防积压（同库同一时刻至多一个待执行的定时任务）；不做更复杂的去重（避免镀金）。
- SMB 库离线：定时入队的扫描任务会失败并记 `error`（FR-29 行为），属真实状态；本功能不为离线 SMB 做特殊抑制。
- 进度真源不变：定时入队的任务与手动入队走同一 FR-29 队列与全局 `ScanStatus`，单 worker 串行不冲突。
