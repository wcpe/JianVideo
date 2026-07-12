# 功能规格：本地离线影视信息推断

> 状态：已实现并完成第二轮复核阻断修复　·　关联 PRD：FR2-031　·　阶段：P2 `0.23.x`　·　复核分支：`feature/p2-fr2-031-complete`

## 1. 背景与目标

P2 需要在不联网的前提下，从文件名和目录本地推断电影片名、剧集季号/集号，并允许人工纠正、可关闭。当前系统只有文件名日期解析、显示名编辑和基础元数据，没有影视推断 schema、置信度、来源、纠正状态或按库分型策略。

目标：

- 建立离线、可解释、可关闭的影视信息推断能力。
- 按 FR2-052 库分型选择 movie/series/home_video 推断策略。
- 允许用户人工纠正，纠正值优先生效且保留审计。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-024（推断开关配置）、FR2-037（backfill 任务）、FR2-040（审计核心）、FR2-052（库分型）。

## 2. 需求（要什么）

- 推断字段：标题、年份、季号、集号、集标题、置信度、来源、规则版本、是否人工确认。
- 支持常见文件名模式：
  - `Movie.Name.2024.1080p.mkv`
  - `剧名.S01E02.标题.mp4`
  - `剧名 第1季 第02集.mp4`
  - 目录名作为标题补充。
- 按库分型：
  - `movie` 优先电影标题/年份。
  - `series` 优先剧名/季/集。
  - `home_video` 默认不做影视推断。
  - `mixed` 使用保守规则。
- 设置项可全局关闭推断，也可按库关闭；设置保存后即时生效，重新启用或调整按库范围时自动为已有媒体补齐缺失推断。
- 人工纠正不改磁盘文件名，只改库内推断/显示字段。
- 时间轴提供“全部 / 已推断 / 自动推断 / 人工纠正 / 未推断”筛选，其中“已推断”为自动推断与人工纠正的并集。
- 展示优先级固定为：人工纠正推断 > `media_files.display_name` > 自动推断 > 原始文件名；无法得到有效标题时不写入推断记录。
- 范围内：规则解析、schema、扫描/任务接入、API、基础 UI、测试。
- 不做（范围外）：联网刮削、海报下载、AI 识别、复杂刮削规则插件。

## 3. 设计（怎么做）

Schema：

- `media_inferences`：`media_id`、`space_id`、`kind`、`title`、`year`、`season`、`episode`、`episode_title`、`confidence`、`source`、`rule_version`、`manual`、`created_at`、`updated_at`。

解析器：

- 纯函数输入：文件路径、文件名、库分型、配置。
- 输出推断字段与置信度；标题为空时不写入推断记录。
- 规则版本写入，便于未来 backfill。

任务：

- 扫描入库时同步推断；手工批量重推断走 FR2-037 `full` 任务，设置变更走 `missing` 增量任务，并以 cursor 每批 100 条处理。
- 媒体记录提交后若即时推断失败，通过 `library` 注入的最小回调持久化单媒体 `media` 补偿任务；任务入库成功后唤醒真实 worker，失败任务最多自动尝试 3 次。
- 补偿 worker 复用自动推断入口，执行时重读当前全局/按库开关并保护人工值；任务编排留在 `api` / `tasks` 层，避免 `library` 反向依赖。
- 推断设置真实变化时原子递增任务代次；运行中的旧代次任务在批次边界停止，重新开启或扩大范围会创建补偿任务。
- 设置保存与补偿任务入队位于同一事务；入队失败时设置与任务均回滚。
- 人工纠正写审计事件，并设置 `manual=true`；后续自动推断不得覆盖。
- 媒体列表通过 `inference=inferred/auto/manual/missing` 服务端筛选推断状态，避免仅过滤当前分页结果。

API：

- `GET /api/library/media/:id/inference`
- `PUT /api/library/media/:id/inference`
- `POST /api/library/inference/backfill`

## 4. 任务拆分

- [x] 定义推断 schema 与规则输出结构。
- [x] 实现 movie/series/mixed/home_video 解析规则。
- [x] 接入扫描与批量 backfill 任务。
- [x] 新增查询、人工纠正、关闭配置 API。
- [x] 前端详情或编辑入口展示和修正推断结果。
- [x] 接入审计事件。
- [x] 补单元测试：中英文季集、年份、目录回退、低置信、关闭开关、展示优先级、不覆盖人工值。
- [x] 补集成测试：扫描后推断、关闭后不推断、backfill 重跑。
- [x] 补真服务 E2E：扫描生成推断、人工纠正后 backfill 不覆盖、按库关闭后不推断并在重新开启后由 worker 补齐。
- [x] 补 handler/API 回归：`inference=inferred` 返回自动与人工并集并排除未推断媒体。
- [x] 补确定性补偿回归：媒体落库成功、首次推断失败、补偿任务持久化、真实 worker 成功补齐。
- [x] 同步本功能规格的实现边界、任务语义与验证状态。

## 5. 验收标准

- 常见电影和剧集文件名样本能产生可解释推断结果。
- `home_video` 库默认不写影视标题推断。
- 全局或每库关闭后，扫描/backfill 不产生新推断；重新启用或调整按库范围后，已有缺失项通过增量任务自动补齐。
- 人工纠正后自动 backfill 不覆盖人工值。
- 时间轴可在全局媒体范围筛选已推断/未推断状态，筛选请求由服务端执行；`inference=inferred` 必须返回自动与人工并集并排除缺失项。
- 媒体已落库但即时推断失败时，必须持久化可唤醒 worker 的单媒体补偿任务，并能在后续真实 worker 执行中补齐。
- 全局关闭、每库关闭、人工纠正、无有效标题不落库均有测试覆盖。
- 推断与人工纠正写审计事件。
- `go test ./...`、推断相关 race、前端全测/typecheck/lint/build 与 FR2-031 Playwright 真服务测试通过。
- Go 单二进制 serve 后，测试库扫描、人工纠正、按库关闭及补偿回填实跑通过。

## 6. 验收证据

- 测试先行：新增 handler/API 与补偿链路测试后，首次执行 `go test ./internal/api -run 'TestMediaInferenceAPIListsInferredAsAutoAndManualUnion|TestCreateMediaFilePersistsInferenceCompensationAndRealWorkerCompletes' -count=1` 因补偿注入接口和单媒体任务模式尚不存在而构建失败；实现后同命令通过。
- handler/API 回归：`TestMediaInferenceAPIListsInferredAsAutoAndManualUnion` 通过真实 Gin 路由验证 `inference=inferred` 返回自动与人工并集并排除 missing。
- 补偿链路回归：`TestCreateMediaFilePersistsInferenceCompensationAndRealWorkerCompletes` 用 SQLite 触发器确定性制造首次推断失败，验证媒体记录保留、补偿任务为 pending 且触发唤醒回调，移除触发器后由真实 `WorkerRegistry.RunPending` 执行成功并补齐推断。
- 全量后端：`go test ./... -count=1` 通过。
- FR2-031 race：`go test -race ./internal/api ./internal/library ./internal/settings ./internal/tasks -run 'Inference|MediaInference' -count=1` 通过。
- 前端相关测试：`npm --prefix frontend test -- src/pages/TimelinePage.test.tsx src/pages/SettingsPage.test.tsx src/utils/media.test.ts` 通过，3 个文件共 53 个测试。
- 前端静态门：`npm --prefix frontend run typecheck`、`npm --prefix frontend run lint` 均通过；`go vet ./...` 通过。
- 真服务 E2E：`npx playwright test e2e/fr2-031-offline-title-inference.spec.ts --workers=1` 通过，Chromium 2/2；串行执行用于避免两个用例同时重置共享真服务数据。

## 7. 风险 / 待定

- 当前版本不单独持久化或展示低置信候选；无法得到有效标题时保留原始文件名。
- 规则解析不可能覆盖所有命名习惯，必须把“可解释、可纠正、可关闭”作为验收重点。
- FR2-031 Playwright 文件在多 worker 下仍会因共享真服务重置端点互相干扰；本轮未扩大范围修改 E2E 基础设施，验收命令固定 `--workers=1`。
