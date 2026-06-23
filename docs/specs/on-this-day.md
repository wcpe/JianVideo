# 功能规格：那年今日回忆流

> 状态：开发中　·　关联 PRD：FR-72　·　分支：feature/fr-72-onthisday

## 1. 背景与目标

为时间轴首页提供「那年今日」回忆能力（FR-72，P7）。基于媒体时间（`media_files.media_time`），自动挑出「往年同一月日」拍摄的媒体，在首页以「X 年前的今天」回忆卡片集中展示，帮助用户重温过往同一天的照片与视频。

与首页既有「继续观看」（FR-44）区块同为时间轴页的平级回忆/快捷区块，复用其轻量查询 + 横向卡片流范式；无回忆时整块不渲染。

## 2. 需求（要什么）

- 回忆筛选：挑出 `media_time` 命中「今天（服务器本地时间）的月-日」、但年份不等于今年的媒体（即往年的同一天）。
- 排除项：`media_time` 为空的记录（不回退入库时间，避免混入入库时间噪声）；软删（`deleted_at` 非空）记录；今年当天的记录（不算「那年今日」）。
- 排序与限量：按 `media_time` 倒序（最近的年份在前），返回条数受 `limit` 控制（默认 12，上限沿用现有继续观看上限 50）。
- 首页展示：时间轴页在「继续观看」旁新增平级「那年今日」区块，横向卡片流展示缩略图与展示名，按项标注「X 年前的今天」；点击进入播放页 `/play/:id`。回忆为空时该区块不渲染。
- 不做（范围外）：自定义回忆日期（只看「今天」）、按相册/标签维度的回忆、回忆推送/通知、图片与视频的差异化交互（统一点击进入）、跨设备/多账号区分。

## 3. 设计（怎么做）

复用既有分层：`api`（HTTP）→ `library.Service`（业务）→ `db`（GORM）。回忆查询作用于 `media_files` 表，归属 `library.Service`（与继续观看同源）。无新数据模型、无新架构决策、不写 ADR、不引新依赖。

### 后端服务（`internal/library/watch_state.go` 复用文件追加）

- `ListOnThisDay(limit int) ([]MediaFile, error)`：仿 `ListContinueWatching` 的轻量写法。
  - 「今天」按服务器本地时间 `time.Now()` 取月日（`MM-DD`）与年份（`YYYY`），避免时区/客户端分歧。
  - SQLite 条件：`media_time IS NOT NULL AND deleted_at IS NULL AND strftime('%m-%d', media_time) = ? AND strftime('%Y', media_time) != ?`。
  - 排序 `media_time DESC`，`Limit(limit)`；`limit` 小于 1 回退默认 12、超上限收敛到 50（复用 `continueWatchingMaxLimit`）。

### 后端端点（`internal/api/watch_handler.go` 复用文件追加）

- `GET /api/library/on-this-day?limit=N`：返回 `{"items": [...]}`（仿 `ContinueWatching` handler）。
- 路由注册于 `router.go` 媒体库分组，紧邻继续观看端点。

### 前端（`frontend/src`）

- `api/library.ts`：补 `getOnThisDay()` 的 real + mock 双实现与导出（沿用 `VITE_USE_MOCK` 切换）。mock 版按「往年同月日」过滤 `mockMediaFiles`。
- 新增组件 `components/OnThisDay.tsx`：仿 `ContinueWatching` 横向卡片流，挂载时拉取回忆列表，展示名用 `mediaDisplayName`，按项算「X 年前的今天」（今年减媒体年份），点击 `navigate('/play/:id')`。列表为空时整块不渲染。
- `pages/TimelinePage.tsx`：在 `<ContinueWatching/>` 旁新增平级 `<OnThisDay/>`。

## 4. 任务拆分

- [x] service 层 `ListOnThisDay` + 单测（命中往年同月日、排除今年、排除软删、排除 media_time 空、limit/排序）
- [x] handler + 路由 + 端点测试
- [x] 前端 api 双实现 + 导出
- [x] 首页「那年今日」区块组件 + 测试（有数据渲染、空则不渲染）
- [x] 文档同步：PRD 状态、API.md、ARCHITECTURE.md、CHANGELOG

## 5. 验收标准

- `ListOnThisDay` 仅返回 `media_time` 命中「今天月日」且年份非今年、未软删、`media_time` 非空的媒体，按 `media_time` 倒序、受 limit 限制（service 测试覆盖）。
- `GET /api/library/on-this-day` 返回 `{"items": [...]}`，语义同上（handler 测试覆盖）。
- 前端：首页「那年今日」区块在有回忆时展示卡片并可点击进入播放，无回忆时不渲染（组件 + 测试覆盖）。
- 后端 `go build ./...` + `go vet ./...` + `go test ./internal/library/... ./internal/api/...` 全绿；前端 `npx tsc --noEmit` + `npx vitest run` + `npm run build` 全绿。
- 手动验收（待真机验）：库内存在往年同一天拍摄的媒体时，首页出现「那年今日」回忆卡片并可点击进入。

## 6. 风险 / 待定

- 闰年 02-29：仅当今天恰为 02-29 时才会匹配 02-29 的历史媒体，平年不会误命中（`strftime('%m-%d')` 字符串精确比较，符合预期）。
- `media_time` 存储为 UTC、按 `strftime` 提取的是其字面月日；与「服务器本地今天」对比可能在跨日临界出现 ±1 天偏差。当前阶段按字面比较即可，跨时区精确归一不在本期范围。
