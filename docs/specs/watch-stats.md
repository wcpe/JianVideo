# 功能规格：观看热力与统计（FR-75）

> 状态：开发中　·　关联 PRD：FR-75　·　分支：feature/fr-75-stats

## 1. 背景与目标

媒体库已积累观看状态（FR-44：续播位置 / 是否看完 / 最近观看时间），但缺少把这些散落数据汇总成「全局视图」的页面。本功能（P7）复用既有观看状态列，新增一个观看统计页，让用户一眼看清：看了多少 / 还剩多少、最近一段时间的观看活跃度、续播停留在视频哪个阶段、各存储库与各格式的观看分布、以及最常看的若干视频。

属于 P7 增强能力，纯展示与聚合，不改变播放 / 续播既有行为。

## 2. 需求（要什么）

新增「观看次数」记录与「观看统计」聚合端点 + 前端统计页。

- 范围内：
  - **观看次数**：`media_files` 新增 `view_count` 列（默认 0）。**看完计一次**——`MarkWatched` 时 `view_count = view_count + 1`；`UpdateWatchPosition`（约 10s 一次的位置上报）**不**计数，避免重复累加。
  - **统计聚合端点** `GET /api/library/stats`，返回以下维度（均仅统计未软删 `deleted_at IS NULL` 的媒体）：
    1. 已看 / 未看 / 总数计数（`watched` 真假分组）。
    2. 最近观看时间线：`last_watched_at` 非空，按本地时区天分桶（最近 N 天），每天观看媒体数。
    3. 续播位置热力：`duration>0 AND last_position>0` 的媒体，按 `last_position/duration` 比例分 10 档（0–10% … 90–100%），每档媒体数。
    4. 各存储库观看分布：按 `library_id` 分组，已看媒体数（含库 label）。
    5. 各格式观看分布：按 `format` 分组，已看媒体数。
    6. 观看次数 Top N：`view_count>0` 按次数倒序取前 N。
  - **统计页** `/stats`（导航「统计」`IconChartBar`）：用 Mantine `<Progress>` / CSS Grid 热力 / 内联 SVG 展示上述各维度，**不引图表库**。
- 不做（范围外）：
  - 不记录每次观看的明细日志表（只在 `media_files` 累加计数，YAGNI、守真源不变量）。
  - 不做时段 / 时长统计、不做导出、不做跨用户（本项目单用户）。
  - 位置上报不计观看次数（只在「看完」计数，口径单一）。

## 3. 设计（怎么做）

- **数据模型**：`internal/db/models/media_file.go` 的 `MediaFile` 加 `ViewCount int gorm:"default:0"`。`Duration`/`LibraryID`/`Format` 已存在，无需新增。AutoMigrate 自动建列（`MediaFile` 已在 `main.go` 迁移列表），main.go 无需改。
- **计数写入**：`internal/library/watch_state.go` 的 `MarkWatched` 在原 `Updates` map 基础上把 `view_count` 用 `gorm.Expr("view_count + 1")` 自增（与置 `watched`/清零位置同一次 UPDATE 原子完成）。
- **聚合查询**：`internal/library` 新增 `stats.go`，一个 `WatchStats` 结果结构与 `GetWatchStats()` 方法，纯查询现有列、全部带 `deleted_at IS NULL`。时间线分桶复用 `watch_state.go` 既有 `strftime(..., 'localtime')` 口径。各维度独立查询、组装为结构体返回（无副作用）。
- **端点**：`internal/api` 新增 `stats_handler.go` 的 `WatchStats` handler，`router.go` 在 `/api/library` 组注册 `GET /stats`。
- **前端**：
  - `frontend/src/api/library.ts`（或新建 `stats` 模块）加 `getWatchStats()`（real/mock 双实现，沿用既有约定）。
  - `frontend/src/types/index.ts` 加 `WatchStats` 及子结构类型；`MediaFile` 加可选 `view_count`。
  - 新页 `frontend/src/pages/StatsPage.tsx`：已看/未看用 `<Progress>` 占比条；时间线、续播位置热力用 CSS Grid 单元格按桶值映射背景色深浅；Top N 列表用卡片 + `<Progress>`。
  - `App.tsx` 注册 `/stats` 路由；`AppLayout.tsx` navItems 加「统计」项（`IconChartBar`）。
- 无新架构决策：加列 + 只读聚合端点不构成 ADR（沿用 FR-44 观看状态真源、单一真源不变量）。

## 4. 任务拆分

- [ ] 后端测试先行：`MarkWatched` 使 `view_count+1`、`UpdateWatchPosition` 不计数；`GetWatchStats` 各维度断言。
- [ ] `MediaFile` 加 `ViewCount`；`MarkWatched` 自增计数。
- [ ] `library/stats.go`：`WatchStats` 结构 + `GetWatchStats`。
- [ ] `api/stats_handler.go` + `router.go` 注册 `GET /api/library/stats`。
- [ ] 前端：types、api（real/mock）、`StatsPage`、路由与导航项。
- [ ] 前端测试：`StatsPage` 喂 mock stats 渲染各维度。
- [ ] 文档同步：PRD 状态、ARCHITECTURE（media_files 字段表加 `view_count` + 说明）、API.md（新端点）、CHANGELOG 未发布段。

## 5. 验收标准

- 后端：`go build ./...`、`go vet ./internal/...` 通过；`internal/library`、`internal/api` 受影响包 `go test` 全绿，含 `MarkWatched` 计数与 `GetWatchStats` 各维度用例。
- 前端：`npx tsc --noEmit`、`npx vitest run`、`npm run build`（生产构建）全绿；`StatsPage` 测试以 mock stats 断言各维度渲染。
- 计数口径正确：仅「看完」计一次，10s 位置上报不计数（单测覆盖）。
- 各聚合维度仅统计未软删媒体（单测覆盖软删排除）。

## 6. 风险 / 待定

- 续播位置比例可能 >1（位置上报晚于 duration 修正等边界）：分桶时对比例做 `[0,1]` 收敛，落入最后一档。
- 时间线天数固定窗口（最近 N 天），N 取常量、不做可配置（YAGNI）。
