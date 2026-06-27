# 功能规格：时间轴苹果风重设计（Timeline Apple-style Redesign）

> 状态：开发中　·　关联 PRD：FR-120（扩 FR-14/FR-32/FR-68/FR-72）　·　分支：feature/timeline-apple-redesign

## 1. 背景与目标

时间轴页（`/timeline`，FR-117 后由 `/` 迁来）现有右侧可拖动 scrubber（FR-68，**仅拖动**才显示单张缩略图浮层）与日/月/年缩放（FR-32）。本特性按苹果「照片」App 体验升级：右侧时间标尺**鼠标靠近（hover）即弹出该时段缩略图预览**、缩放扩为年/月/日/所有、方形密铺网格更紧致，并在顶部新增「最近查看」回忆区块（与既有「那年今日」FR-72、「继续观看」FR-44 并列）。属第十二期。

## 2. 需求（要什么）

- **右侧时间标尺悬停预览**：把 FR-68 的「仅拖动显单张」升级为「**hover 即在指针处弹出该时段（按当前缩放粒度）预览**——日期 + 数量 + 该时段前若干张缩略图九宫格」；点击/松手仍跳转到对应分组。保留键盘可达与拖动跳转。
- **缩放**：现有 `日/月/年` 扩为 `年/月/日/所有`（「所有」= 不分组的方形密铺）。
- **顶部「最近查看」**：横向卡片流展示最近打开过的媒体（图片+视频），点击进入查看/播放；空则不渲染。与「那年今日」「继续观看」并列。
- **最近查看数据**（后端）：媒体被打开（详情面板 / 播放页）时记 `last_viewed_at`；新增 `recently-viewed` 端点。
- **网格苹果风**：方形密铺、日期分组头更紧致（在现有 `TimelineView` 上做克制的视觉收紧，不重写其虚拟滚动/选择/批量逻辑）。

- 范围内：上述标尺悬停预览 + 缩放扩展 + 最近查看（含 `viewed_at` 后端）+ 网格视觉收紧。
- 不做（范围外）：改 FR-69 多选 / FR-34 详情面板 / FR-91 批量的既有交互；地图/相册等其他页；把「最近查看」做成可配置/可清除历史（YAGNI）。

## 3. 设计（怎么做）

### 3.1 后端：最近查看（`last_viewed_at`）

- **数据模型**：`models.MediaFile` 加 `LastViewedAt *time.Time`（json `last_viewed_at,omitempty`，gorm 可空 + 索引）。注册进现有 AutoMigrate（列添加，向后兼容、旧库自动加列）。
- **记录端点** `PUT /api/library/media/:id/viewed`：把该媒体 `last_viewed_at` 置为当前时间，200。非法 id → 400，不存在 → 404。
- **列表端点** `GET /api/library/recently-viewed?limit=12`：返回 `last_viewed_at` 非空、未软删的媒体，按 `last_viewed_at` 倒序，`limit` 缺省 12、上限 50。响应 `{ "items": MediaFile[] }`。
- 实现仿现有「继续观看」（`internal/api/watch_handler.go` 的 ContinueWatching / OnThisDay + library 服务查询），口径一致（排除 `deleted_at`）。服务层查询、db 仅读写。
- **去重语义**：「最近查看」与「继续观看」（FR-44，有进度未看完）是不同维度——前者按最近打开排序、不论进度/类型；二者可重叠展示，互不替代。

### 3.2 前端：最近查看

- `api/library.ts`（或对应 api 模块）：`setMediaViewed(id)` → `PUT .../viewed`；`getRecentlyViewed(limit?)` → `GET .../recently-viewed`。
- **记录时机**：媒体被打开时调 `setMediaViewed(id)`——在 `TimelinePage` 的 `handleOpen`（详情面板打开）与 `PlayPage` 挂载（视频播放）各记一次；失败静默（不阻塞打开）。
- `components/RecentlyViewed.tsx`：仿 `OnThisDay`/`ContinueWatching`（横向卡片流 + 缩略图 + 展示名，点击进入；空列表不渲染）。
- `TimelinePage`：顶部「继续观看」「那年今日」旁新增 `<RecentlyViewed />`。

### 3.3 前端：时间标尺悬停预览 + 缩放

- `components/TimelineScrubber.tsx` 升级：
  - 新增 **hover 预览**：指针在轨道上移动（未按下）即按指针 Y 算出目标分组、在指针处弹出预览浮层——分组日期 + 该组媒体数 + 前 N（如 4-6）张缩略图小九宫格。移出轨道隐藏。
  - 拖动行为保留（FR-68）：按下拖动持续更新并松手跳转；hover 与 drag 共用「指针 Y → 分组下标」纯函数（复用 `positionToGroupIndex`）。
  - 键盘可达保留（上下键移动分组并跳转）。
  - 可选：轨道旁按缩放粒度标注时段刻度（年/月标签），不强制。
  - 无障碍：预览浮层 `pointer-events:none`、`role="img"` + `aria-label`（日期 + 数量）。
- `TimelinePage` 缩放 `SegmentedControl`：`日/月/年` 扩为 `年/月/日/所有`。`所有` = 不按日期分组的方形密铺（最紧），其余按粒度分组。`granularity` 类型与 `utils/timeline` 分组逻辑相应扩展（`all` 分支：单组/不分组、方形密铺）。

### 3.4 前端：网格苹果风收紧

- 在 `TimelineView` 现有日期分组网格上做**克制收紧**：方形（`aspect-ratio:1`）密铺、更小间距、日期分组头更轻；**不动**其虚拟滚动、`useMultiSelect`、右键批量、详情打开等既有逻辑（精准修改）。视频角标/选中态保留。

### 3.5 架构

无新 ADR（`last_viewed_at` 为小列添加，同 `last_watched_at` 既有模式；标尺/网格为前端交互增强）。同步 ARCHITECTURE 数据模型（media_files 加列）与 API.md。

## 4. 任务拆分

- [ ] 后端 `MediaFile.LastViewedAt` + AutoMigrate
- [ ] 后端 `PUT /viewed` + `GET /recently-viewed` + 单测（记录置时间、列表倒序、排除软删、limit 夹紧、空库空列表）
- [ ] 前端 `setMediaViewed`/`getRecentlyViewed` + 打开时记录（TimelinePage/PlayPage）
- [ ] 前端 `RecentlyViewed` 组件 + 接入 TimelinePage 顶部 + 测试
- [ ] 前端 `TimelineScrubber` hover 预览升级 + 测试
- [ ] 前端 缩放扩 `所有` + `utils/timeline` all 分支 + 测试
- [ ] 前端 `TimelineView` 方形密铺收紧（不破既有逻辑/测试）
- [ ] 文档同步：PRD（AC-24）、API.md、ARCHITECTURE、CHANGELOG

## 5. 验收标准

- **AC-24**（见 PRD §6）：`/timeline` 右侧时间标尺**鼠标 hover**（非仅拖动）即在指针处弹出该时段日期 + 数量 + 缩略图预览，点击/松手跳转到该分组；缩放支持年/月/日/所有；顶部展示「最近查看」（有数据时横向卡片流、可点击进入，空则不渲染）；打开媒体后其进入「最近查看」。多选/详情/批量既有交互不回归。
- 后端 `PUT /viewed` 置 `last_viewed_at`、`GET /recently-viewed` 倒序排除软删、limit 夹紧、空库空列表——单测覆盖。
- 受影响前端跑生产构建（`npm run build`）+ vitest 全绿（含 `TimelineScrubber`/`TimelinePage` 既有测试更新）。
- 苹果风观感 / hover 手感 / 缩放切换 / 真实缩略图预览**真机走查**待真机。

## 6. 风险 / 待定

- hover 预览频繁触发：指针移动按帧节流（rAF 或节流），避免重渲染抖动；缩略图复用现有 `MediaThumbnail`（带懒加载/骨架）。
- `所有` 密铺在大库（万级）下仍走现有虚拟滚动，不可一次性渲染全部。
- `last_viewed_at` 与 `last_watched_at` 并存：前者覆盖图片+视频的「打开」、后者仅视频播放进度，语义不同、各自独立。
- 记录时机失败静默：`setMediaViewed` 不阻塞媒体打开（打开体验优先于记录）。
