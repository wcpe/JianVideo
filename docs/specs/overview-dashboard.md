# 功能规格：首页概览看板（Overview Dashboard）

> 状态：开发中　·　关联 PRD：FR-117　·　分支：feature/overview-dashboard

## 1. 背景与目标

当前根路由 `/` 是时间轴视图（FR-A/AC-13 确立）。时间轴是「浏览媒体」的入口，但不是「了解系统整体状况」的入口。本特性把首页改为**系统总览数据看板**，让用户进入即看到媒体库规模、构成、观看进度、系统运行与任务状况；时间轴迁至 `/timeline` 作为「浏览」组的一项。属第十二期（首页概览与数据可视化）。

## 2. 需求（要什么）

- 根路由 `/` 渲染「概览」看板（分析大盘式布局），时间轴迁至 `/timeline`。
- 导航「浏览」组顶部新增「概览」项（`IconLayoutDashboard`），其下为「时间轴」（指向 `/timeline`）。
- 看板分区（自上而下）：
  1. **KPI 大数卡**：媒体总数（视频/图片拆分）、视频总时长、占用空间、媒体库数、相册数。
  2. **媒体构成与各库分布**：视频/图片占比条 + 各库 media_count 横条。
  3. **观看概览**：已看/未看进度（复用 `/api/library/stats`）+ 「查看全部 ›」跳 `/stats`。
  4. **系统状态**：FFmpeg 可用、硬件加速、运行时长、内存（复用 `/api/system/info`）+ 巡检问题数（复用 `/api/library/health/status`）。
  5. **任务队列**：扫描进行中进度（复用 `/api/library/scan/tasks`）+ 转码 pending/running/completed 计数（复用 `/api/transcode/tasks`，前端聚合计数）。
  6. **继续观看**：复用现有 `ContinueWatching` 组件。
- 新增后端聚合端点 `GET /api/library/summary`（见 §3 契约），补齐总占用、总时长、视频/图片拆分、各库聚合——现有接口拿不到的维度。

- 范围内：上述看板 + summary 端点 + 路由/导航调整 + AC-13 修订 + ADR-0043。
- 不做（范围外）：折线趋势图（留 FR-118 引 Recharts）；统计页改造（FR-118）；监控页（FR-119）。本特性**不引图表库**，看板只用现有横条/进度/环形（SVG stroke）范式。

## 3. 设计（怎么做）

### 3.1 后端：`GET /api/library/summary`

媒体库总量聚合，一次性查询、避免 N+1。所有计数/求和均 `WHERE deleted_at IS NULL`。

**视频/图片分类口径**（复用全站一致谓词，见 `favorites_tags.go`）：
- 图片：`LOWER(format) IN builtInImageExtensionList()`
- 视频：`LOWER(format) NOT IN builtInImageExtensionList()`

**响应（200）**：
```json
{
  "total": 12480,
  "video_count": 3210,
  "image_count": 9270,
  "total_size": 1979900000000,
  "total_duration": 2311200.0,
  "library_count": 5,
  "by_library": [
    {
      "library_id": 1,
      "label": "电影",
      "media_count": 3460,
      "video_count": 3460,
      "image_count": 0,
      "total_size": 580000000000,
      "total_duration": 1980000.0
    }
  ]
}
```

字段：`total`=未软删媒体总数；`video_count`/`image_count`=按上述口径拆分；`total_size`=`SUM(file_size)`（字节）；`total_duration`=`SUM(duration)`（秒，图片 duration 为 0 不影响）；`library_count`=启用库数（与 `/paths` 口径一致，`enabled=1`）；`by_library`=按 `library_id` 分组的各库聚合（`label` 取自 `library_paths`，LEFT JOIN）。

- 空库：`total=0`、各计数/求和为 0、`by_library: []`，HTTP 200 不报错。
- 实现位置：`internal/library/summary.go`（新增 `Service.GetLibrarySummary()`），handler `internal/api/summary_handler.go`（或并入既有 handler 文件，按现有组织），路由 `lib.GET("/summary", h.LibrarySummary)` 注册于 `internal/api/router.go` 的 `/api/library` 组。
- 架构红线：`db` 仅数据读写；聚合逻辑在 `library` 服务层；`web/api` 仅转发。单次 GROUP BY 取 by_library，避免逐库查询（N+1）。

### 3.2 前端

- `frontend/src/api/summary.ts`：`getLibrarySummary(): Promise<LibrarySummary>`，沿用 `stats.ts` 的 mock/real 双实现范式（`VITE_USE_MOCK`）。
- `frontend/src/types.ts`：新增 `LibrarySummary` / `LibrarySummaryRow` 类型，字段对齐契约。
- `frontend/src/pages/OverviewPage.tsx`：看板页（A 版布局）。复用 Mantine `Card`/`Progress`/`SimpleGrid` 与现有范式；环形进度用 SVG stroke（无图表库）。各数据源失败时该卡降级（占位/零值），不整页崩。复用 `ContinueWatching`、`MediaThumbnail`、`formatBytes`/`formatUptime`（可从 `SystemPage` 提取为 `utils/format.ts` 公共函数，避免复制粘贴）。
- 路由 `frontend/src/App.tsx`：`/` → `OverviewPage`；新增 `/timeline` → `TimelinePage`。
- 导航 `frontend/src/components/AppLayout.tsx`：`navItems` 新增 `{ path: '/', label: '概览', icon: IconLayoutDashboard }`，原时间轴项路径由 `/` 改 `/timeline`；`navGroups` 的「浏览」组顺序：概览、时间轴、目录、相册、地图、统计。命令面板（复用 navItems）随之更新。`isNavActive('/')` 精确匹配已兼容。

### 3.3 架构决策

见 ADR-0043（首页由时间轴改为概览看板，取代 FR-A/AC-13 确立的「时间轴=首页」约定）。

## 4. 任务拆分

- [ ] 后端 `GetLibrarySummary()` 聚合 + 单测（空库/多库/视频图片拆分/总大小时长/by_library）
- [ ] 后端 handler + 路由 `GET /api/library/summary`
- [ ] 前端 `api/summary.ts` + 类型 + mock
- [ ] 前端 `OverviewPage` 看板各分区 + 组件测试
- [ ] 路由 `/`=概览、`/timeline`=时间轴；导航「浏览」组加「概览」
- [ ] 文档同步：PRD（AC-13 修订 + AC-21）、ADR-0043、API.md、ARCHITECTURE、CHANGELOG

## 5. 验收标准

- **AC-21**（见 PRD §6）：`/` 渲染概览看板各分区；空库各卡零值不报错；`/api/library/summary` 单次聚合返回 total/video_count/image_count/total_size/total_duration/library_count/by_library。
- 时间轴在 `/timeline` 正常（按添加时间倒序、图片预览、视频进播放页）；导航「概览」「时间轴」两项均可达且激活态正确。
- 后端 summary 聚合单测覆盖：空库（全零）、多库分组、视频/图片拆分口径与 `favorites_tags` 一致、`SUM(file_size)`/`SUM(duration)` 正确。
- 前端组件测试覆盖：各卡数据渲染、空库零值、跳转 `/stats`、`/timeline` 可达。
- 受影响前端跑生产构建（`tsc -b` / `npm run build`）类型通过，不只 vitest。
- 视觉走查（看板观感/响应式）待真机。

## 6. 风险 / 待定

- 视频/图片口径依赖 `builtInImageExtensionList()`：自定义图片后缀不计入「图片」（与 `favorites_tags` 现有粗筛口径一致，刻意保持一致，不在本特性纠正）。
- `total_duration` 仅对有 duration 的视频有意义；图片/探测失败者为 0，求和天然忽略。
- 大库（万级）下 summary 为纯聚合 SQL（COUNT/SUM + GROUP BY），走 `deleted_at`/`library_id` 索引，满足 NFR-08。
