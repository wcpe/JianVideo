# 功能规格：统计页内容趋势扩展（Stats Content Trends）

> 状态：开发中　·　关联 PRD：FR-118（增强 FR-75）　·　分支：feature/stats-content-trends

## 1. 背景与目标

现有 `/stats`（观看统计，FR-75）只呈现「当前结果」（已看/未看、热力、Top 等），缺少「随时间的趋势」。本特性把统计页扩为**结果 + 趋势**并存：顶部分「观看 / 媒体」两 tab，每 tab = 当前值卡（含 sparkline）+ 时序折线图。引入 Recharts 画折线（见 ADR-0045），并为「监控页」（FR-119）复用同一图表库。属第十二期。

## 2. 需求（要什么）

- `/stats` 顶部新增一级 tab：**观看 / 媒体**。
- **观看 tab**：当前值卡（已看 / 未看 / 追看中）+「观看活跃趋势」折线（复用现有 `stats.recent_timeline` 按天观看数）+ 保留现有「续播位置热力 / 各库 / 各格式 / Top 榜」。
- **媒体 tab**：当前值卡（媒体总数 / 视频 / 图片 / 总时长 / 占用，**复用 `/api/library/summary`**，FR-117）+「媒体增长曲线」（累计媒体数）+「累计容量 / 时长增长」+ 媒体构成（视频/图片占比 + 各库 `media_count`，复用 summary）。
- 当前值卡带 sparkline（迷你折线）与涨跌（可由趋势序列末端推算）。
- 新增后端端点 `GET /api/library/trends` 提供「按天新增媒体」序列（见 §3.1），前端据此算累计增长。

- 范围内：上述两 tab + trends 端点 + 引入 Recharts + 统计页改造。
- 不做（范围外）：系统资源监控（FR-119）；改动观看统计的既有聚合口径（FR-75 不变，仅在其上加趋势视图与 tab 容器）。

## 3. 设计（怎么做）

### 3.1 后端：`GET /api/library/trends`

「按天新增媒体」全时段序列，供前端算累计增长曲线（媒体数 / 容量 / 时长）。

**响应（200）**：
```json
{
  "media_added": [
    {"date": "2026-05-01", "count": 10, "size": 1000000000, "duration": 3000.0},
    {"date": "2026-05-03", "count": 5,  "size": 500000000,  "duration": 1500.0}
  ]
}
```

- `media_added`：按 `added_at` 本地时区天分桶，仅含有新增的天，**升序**。`count`=当天新增媒体数、`size`=`SUM(file_size)`、`duration`=`SUM(duration)`。
- 全程 `WHERE deleted_at IS NULL`。空库返回 `{"media_added": []}`、HTTP 200。
- 时区口径与 `stats.go` 的 `recent_timeline` 一致（`strftime('%Y-%m-%d', added_at, 'localtime')`）。
- 返回全时段（不分页）：天数有界（按日聚合），前端按 range（近 30/90 天 / 1 年 / 全部）切 x 轴视图、并累加得累计曲线（baseline 从 0 起即首个有数据日的累计，符合「增长曲线」语义）。
- 实现：`internal/library/trends.go`（`Service.GetMediaTrends()`，一次 GROUP BY），handler + 路由 `lib.GET("/trends", h.MediaTrends)`。聚合在 library 服务层、db 仅读写。

> 观看活跃趋势**不新增端点**：复用现有 `GET /api/library/stats` 的 `recent_timeline`（FR-75，已是按天观看数）。

### 3.2 前端

- 依赖：`npm install recharts`（ADR-0045，用户已批准）。仅在统计/监控页用。
- `frontend/src/api/trends.ts`：`getMediaTrends(): Promise<MediaTrends>`，mock/real 双实现（仿 `stats.ts`）。类型 `MediaTrends { media_added: MediaTrendPoint[] }`、`MediaTrendPoint { date; count; size; duration }`。
- `frontend/src/pages/StatsPage.tsx`：改为 Mantine `Tabs`（观看 / 媒体），URL query `?tab=` 记忆（仿 ConsolePage）。
  - 抽出可复用小组件：`TrendChart`（Recharts `LineChart`/`AreaChart` + `Tooltip`/`XAxis`/`YAxis`，响应式 `ResponsiveContainer`，品牌紫 stroke）、`MetricCard`（当前值 + sparkline + 涨跌）。放 `frontend/src/components/`。
  - **观看 tab**：`MetricCard`（已看/未看/追看中）+ 「观看活跃趋势」`TrendChart`（消费 `stats.recent_timeline`）+ 现有续播热力/各库/各格式/Top 榜（原样保留，逐卡空守卫沿用 FR-88）。
  - **媒体 tab**：`MetricCard`（总数/视频/图片/总时长/占用，消费 `getLibrarySummary`）+「媒体增长曲线」（累计 `count`）+「累计容量/时长增长」（累计 `size`/`duration`）`TrendChart`（消费 `getMediaTrends`，前端累加）+ 媒体构成（视频/图片 + by_library 横条，消费 summary）。
  - 空/稀疏态：沿用 FR-88 —— 空库整页 `EmptyState`；趋势序列为空时该图显「暂无数据」占位，不渲空图。
- Recharts 颜色/字号用主题 token / `var(--mantine-color-purple-*)`，暗色适配（坐标轴/网格线用 `--mantine-color-dimmed`/`default-border`）。

### 3.3 架构决策

见 ADR-0045（前端引入 Recharts 图表库，统计页与监控页共用）。

## 4. 任务拆分

- [ ] 后端 `GetMediaTrends()` + 单测（空库/多日分桶/SUM/软删排除/升序）
- [ ] 后端 handler + 路由 `GET /api/library/trends`
- [ ] 前端 `npm install recharts` + ADR-0045
- [ ] 前端 `api/trends.ts` + 类型 + mock
- [ ] 前端 `TrendChart` / `MetricCard` 复用组件 + 测试
- [ ] 前端 StatsPage 分「观看/媒体」tab，接入趋势图与当前值卡 + 测试
- [ ] 文档同步：PRD（AC-22）、ADR-0045、API.md、ARCHITECTURE、CHANGELOG

## 5. 验收标准

- **AC-22**（见 PRD §6）：`/stats` 顶部「观看 / 媒体」两 tab 可切换并记忆（`?tab=`）；观看 tab 含已看/未看/追看中当前值卡 + 观看活跃趋势折线 + 既有热力/分布/Top；媒体 tab 含总数/视频/图片/时长/占用当前值卡 + 媒体增长 / 累计容量时长折线 + 媒体构成；空库整页空态、空趋势显占位不报错。
- 后端 `GET /api/library/trends` 单测：空库（空数组）、多日分桶升序、`count`/`SUM(size)`/`SUM(duration)` 正确、软删排除。
- 折线图可 hover 显示该点精确值（Recharts `Tooltip`）。
- 受影响前端跑生产构建（`npm run build`）类型通过 + vitest 全绿（含 StatsPage 既有测试更新）。
- 视觉走查（图表观感/暗色/响应式）待真机。

## 6. 风险 / 待定

- Recharts 体积：自托管单用户场景可接受（见 ADR-0045 权衡）；仅统计/监控页引入，按需 code-split 由 Vite 处理。
- 累计曲线 baseline 从首个有数据日起累加（增长曲线语义），非「全库历史绝对累计 + 窗口外基线」——刻意从 0 起，简单且符合「增长」直觉。
- sparkline 数据：观看卡用 `recent_timeline`、媒体卡用 `media_added` 末端窗口；序列过短时 sparkline 退化为短线或不显，不报错。
