# 功能规格：定期扫描、增量更新与目录事件监听

> 状态：已交付@v0.23.0　·　关联 PRD：FR2-027　·　阶段：P2 `0.23.x`　·　分支：`codex/fr2-027-scan-watch`

## 1. 背景与目标

现有系统已有 fsnotify watcher、SMB 轮询、全量/增量扫描、定时扫描和扫描任务 SSE，但 watcher 事件未进入统一任务队列，删除/重命名路径处理与软删除/审计边界不完整，大库 full reconcile 使用路径集合方式不适合 1000 万资产。FR2-027 要让增删改名入队、状态可查、不阻塞播放浏览。

目标：

- 将扫描、目录事件、定期任务接入 FR2-037 统一任务队列。
- 建立变更事件归一、debounce、增量扫描、删除/重命名处理和审计。
- 在大库下避免全量路径集合导致内存和长事务风险。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-024（扫描周期配置）、FR2-037（任务队列）、FR2-040（审计核心）、FR2-052（库分型）、FR2-025（媒体类型规则）。

## 2. 需求（要什么）

- 支持配置定时扫描周期，保存后热更新。
- fsnotify 本地事件与 SMB 轮询事件统一归一为扫描变更任务。
- 增量扫描只处理变更路径；文件修改只产出 `ScanChange(modified)` 并调用元数据、缩略图、hash 的失效 hook，具体 stale 字段分别由 FR2-030、FR2-028、FR2-061 拥有。
- 删除或丢失文件默认标记为 `missing` 并从常规列表隐藏，不物理删除 DB 记录；用户确认删除后才进入回收站/软删；全程写审计。
- 重命名尽量识别为路径更新；无法可靠识别时按删除 + 新增处理。
- 全量 reconcile 分批执行，有进度、可取消、可重试，不阻塞播放浏览。
- 所有任务 Space scoped，并按库分型和媒体类型规则执行。
- 范围内：事件归一、任务入队、增量扫描、分批 reconcile、状态查询、测试。
- 不做（范围外）：跨机器分布式监听、SMB 实时事件协议、复杂内容指纹重命名匹配。

## 3. 设计（怎么做）

事件归一：

- 定义 `ScanChange`：`space_id`、`library_id`、`path`、`op`、`observed_at`。
- watcher/轮询/scheduler 都只提交 `ScanChange` 或扫描任务，不直接执行重活。
- debounce 合并短时间内同一路径事件。

任务：

- 任务类型：`library.scan.full`、`library.scan.incremental`、`library.scan.reconcile`。
- payload 包含库 ID、路径前缀、变更类型、扫描模式。
- 扫描 worker 从 FR2-037 领取，写进度与 checkpoint。

Reconcile：

- 按目录或路径游标分批，不一次性加载全库路径集合。
- 对缺失文件标记 `file_state=missing`，后续由回收站/审计流程处理；reconcile 不物理删除文件或数据库记录。

API：

- 旧 `/api/library/scan/:id` 保留，内部改为入队并返回任务 ID。
- 旧 `POST /api/library/scan/:id?mode=full|incremental` 语义保留，响应兼容旧字段并新增统一任务 ID。
- 任务状态统一走 `/api/tasks`，旧 `/api/library/scan/tasks` 做兼容映射。

## 4. 任务拆分

- [x] 定义扫描变更模型与事件归一逻辑。
- [x] 将手动扫描、定时扫描、watcher 事件接入统一任务队列。
- [x] 实现增量扫描刷新已变更文件元数据。
- [x] 重写 full reconcile 为分批游标式流程。
- [x] 删除/缺失/重命名写审计事件。
- [x] 保留旧扫描 API 兼容层，包括 `mode=full|incremental` 查询参数。
- [x] 补单元测试：事件归一、debounce、扫描模式、路径归一。
- [x] 补集成测试：临时库 create/modify/delete/rename、重启恢复、取消任务。
- [x] 补 E2E：手动扫描入队、进度可查、列表刷新。
- [x] 补 Benchmark：大库 full reconcile 与增量扫描耗时/内存。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 手动扫描和定时扫描只入队，不在 HTTP 请求中执行重扫描。
- 新增、修改、删除、重命名文件在集成测试中能触发正确入库/刷新/标记。
- 文件丢失先标记 `missing` 并从常规列表隐藏；用户确认删除后才软删进回收站；reconcile 不物理删除。
- 全量 reconcile 分批执行，任务进度可查、可取消、可重试。
- 旧 `POST /api/library/scan/:id?mode=full|incremental` 仍可用，响应兼容旧字段并包含统一任务 ID。
- 删除/重命名产生审计事件。
- 大库 Benchmark 不出现一次性全路径集合加载。
- `go test`、扫描集成测试、Playwright 扫描 E2E、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，临时媒体库扫描与进度查询实跑通过。

## 6. 风险 / 待定

- SMB 轮询无法提供高保真重命名事件，需接受删除 + 新增的降级路径。
