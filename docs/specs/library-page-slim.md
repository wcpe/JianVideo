# 功能规格：存储库管理页精简

> 状态：开发中　·　关联 PRD：FR-23　·　分支：feature/fr-23-libpage

## 1. 背景与目标
当前 `/library-manager` 页同时承担两件事：管理存储库目录（增删、扫描、自定义后缀）与浏览/操作页内媒体文件列表（`MediaTimeline`，含删除、重命名、图片预览）。媒体文件的浏览职责已由 `/`（时间轴）与 `/browse`（目录视图）承担，管理页内再塞一份完整媒体列表造成职责重叠、页面冗长。

本功能属第二期（P2），目标是把管理页收敛为「纯存储库管理」：每个库一张卡片，展示该库的扫描进度与已索引媒体数量；点击卡片直接跳到该库的目录视图。

## 2. 需求（要什么）
- 范围内：
  - 管理页移除页内媒体文件列表（`MediaTimeline`）及其关联的媒体删除 / 重命名 / 图片预览逻辑。
  - 存储库卡片展示该库「已索引媒体数量」。
  - 扫描进度保留（全局扫描进度条 + 卡片扫描按钮的加载态）。
  - 卡片可点击，跳转到 `/browse` 并定位到被点击库的根目录（带 `library_id` 与起始 `path`）。
  - 后端 `GET /api/library/paths` 响应为每个库补 `media_count` 字段。
- 不做（范围外）：
  - 不动时间轴页 `/` 与目录视图页 `/browse` 自身的浏览交互。
  - 不实现软删除 / 回收站（FR-25）；但媒体数量统计预先排除已软删记录（`deleted_at IS NULL`），与未来软删一致。
  - 不新增「每库媒体数量」独立端点（在既有列表响应补字段即可，避免多一次往返）。

## 3. 设计（怎么做）
- 后端：
  - `LibraryPath` 模型为基线冻结字段，不改。新增响应 DTO `LibraryPathView`（含模型字段 + `media_count`）。
  - `library.Service` 新增 `ListLibraryPathViews()`：先取所有库，再对 `media_files` 按 `library_id` 分组计数（一次 `GROUP BY` 查询，排除 `deleted_at IS NULL`），避免按库 N+1。
  - `Handler.ListLibraryPaths` 改为返回 `LibraryPathView` 列表，JSON 仍为 `{ "items": [...] }`，仅每项多 `media_count`，向后兼容。
- 前端：
  - `LibraryPath` 类型新增可选 `media_count`；mock 数据补该字段。
  - `LibraryPathManager` 卡片展示媒体数量，整卡可点击触发 `onOpenLibrary(path)`。
  - `BrowsePage` 读取路由查询参数 `library_id`/`path`，存在时用其初始化浏览；否则维持「首个库根目录」默认。
  - `LibraryManagerPage` 删除 `MediaTimeline` 及媒体删除 / 重命名 / 预览相关 state 与回调，卡片点击 `navigate('/browse?library_id=<id>&path=<encodedPath>')`。

## 4. 任务拆分
- [ ] 后端：`ListLibraryPathViews` + `media_count`（先写 service / API 测试，红→绿）
- [ ] 前端：类型 + mock + MSW handler 补 `media_count`
- [ ] 前端：`LibraryPathManager` 卡片展示数量 + 可点击跳转
- [ ] 前端：`BrowsePage` 支持库定位查询参数
- [ ] 前端：`LibraryManagerPage` 移除媒体列表（页面测试红→绿）
- [ ] 文档同步：PRD 状态、API、ARCHITECTURE、CHANGELOG

## 5. 验收标准
- 管理页只展示存储库卡片 + 扫描进度 + 每库媒体数量，不再渲染页内媒体文件列表（`MediaTimeline` 不出现）。
- 点击某库卡片跳转到 `/browse` 并定位到该库根目录（`library_id` 与起始 `path` 正确传递）。
- `GET /api/library/paths` 每项含 `media_count`，值等于该库未软删的 `media_files` 行数。
- 后端 `go test ./internal/library/... ./internal/api/...` 全绿；前端 `npm run build` 与 `npm run test` 全绿。

## 6. 风险 / 待定
- `media_count` 统计需与未来软删（FR-25）口径一致，已约定按 `deleted_at IS NULL` 计数。
- 卡片整体可点击与卡片内「浏览 / 扫描 / 删除」按钮、后缀输入并存，需阻止子操作冒泡触发卡片跳转。
