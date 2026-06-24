# 功能规格：浏览页信息架构 B（FR-101）

> 状态：开发中　·　关联 PRD：FR-101（扩 FR-60/FR-75）　·　分支：feature/fr-101-page-ia-b

## 1. 背景与目标

第九期（P9）界面信息架构与可视化打磨的第二批，复用已落地的设计 token（FR-92）与品牌紫（FR-93）。
解决三处「信息架构 / 可视化粗糙」的体验问题，纯前端、不动后端、不引新依赖：

- **系统诊断页键值排版**：`InfoRow` 用 `Group justify="space-between"` 把 label 与 value 左右拉满，
  中间出现巨大空隙、长路径换行难读。
- **统计页可视化**：自建 CSS 图表配色/刻度/对齐不够精致。
- **回收站 / 巡检 / 重复项三页的媒体列表行**：各自手写、设计朴素、不一致。

## 2. 需求（要什么）

- **系统诊断键值（`SystemPage.tsx`）**：键值改为「定宽 label 的紧凑两列 / 定义列表」——
  label 定宽、value 紧贴其后（不再左右拉满留巨大空隙）；路径 / 版本号等用 **monospace 等宽字体**并可截断，
  `title` 挂全文（保留 FR-97 已有的悬停全文提示）。
- **统计页（`StatsPage.tsx`）**：自建 CSS 图表（进度条 / 热力方块 / 时间线）配色统一为品牌紫阶，
  刻度 / 间距 / 对齐精修，更精致——**仍不引图表库**。
- **媒体行统一组件**：新建 `components/MediaRow.tsx`（缩略图 + 信息 + 操作 / 勾选），
  在 `RecyclePage.tsx` / `InspectPage.tsx` / `DuplicatesPage.tsx` 三页复用，保留各页既有操作回调
  （还原 / 删除 / 勾选）。

- 范围内：上述三块的前端排版 / 可视化 / 组件抽取。
- 不做（范围外）：改后端、引新依赖、改 `MediaThumbnail.tsx`（另一并行 FR 拥有其改动）、
  重排页面整体布局、新增页面或路由。

## 3. 设计（怎么做）

- **InfoRow 重排**：由 `Group justify="space-between"` 改为定宽 label 列 + value 列的紧凑行
  （label 定宽 ~120px、`flex-shrink:0`，value 紧随其后），新增可选 `mono` prop——为真时
  value 用等宽字体（`--mantine-font-family-monospace`）并可截断。系统页对路径 / 版本 / PID 等传 `mono`。
- **统计页配色精修**：热力色阶、时间线条、各分布进度条统一走品牌紫 token（`--mantine-color-purple-*`），
  精修刻度文字 / 间距 / 对齐；已有 `data-testid`（`heat-cell-*`）与 aria-label 保留，不破坏既有测试。
- **MediaRow 组件**：单一展示组件，props 驱动——`mediaID` + `fileName`（喂给现有 `MediaThumbnail`）、
  主标题 / 副标题、可选右侧 `actions` 节点、可选 `selectable` + `selected` + `onToggle`（渲染 Checkbox）。
  根节点保留 `.mantine-Card-root` 类（既有测试以 `.closest('.mantine-Card-root')` 定位），
  直接复用 `MediaThumbnail`，不动其 API / 实现。
  - 回收站：缩略图 + 文件名 + 右侧「还原」按钮。
  - 重复项：缩略图 + 文件名 + 勾选。
  - 巡检：问题项无缩略图（HealthIssue 非 MediaFile，源文件可能丢失），仍用统一行的勾选 + 双行标题 / 详情结构，缩略图位缺省。

## 4. 任务拆分

- [ ] 测试先行：MediaRow 单测 + 三页改用 MediaRow 后既有/新增断言；系统页 InfoRow 定宽两列 + monospace 断言；统计页品牌紫断言。
- [ ] 实现 `components/MediaRow.tsx`，三页接入。
- [ ] 重排 `SystemPage.tsx` 的 `InfoRow`（定宽 label + mono value）。
- [ ] 精修 `StatsPage.tsx` 图表配色 / 刻度 / 对齐。
- [ ] 文档同步：PRD 状态、CHANGELOG 未发布段末尾追加。

## 5. 验收标准

- `frontend/` 下 `npm run build`（tsc -b）+ `npm run test`（vitest）全绿。
- 系统诊断键值为定宽两列、路径 / 版本用 monospace（自动化测试断言）。
- 统计图表用品牌紫（自动化测试断言）。
- 三页用统一 `MediaRow` 渲染（自动化测试断言），既有 SystemPage/StatsPage/RecyclePage/InspectPage/DuplicatesPage 测试全绿。
- 真机（待用户确认）：系统页键值不再左右拉满、统计精致、三页媒体行一致——标「待真机验」。

## 6. 风险 / 待定

- 既有页面测试用 `.closest('.mantine-Card-root')` 与特定 aria-label 定位元素，MediaRow 须保留该类与无障碍标签，避免回归。
- 巡检页问题项无缩略图，MediaRow 需支持「无缩略图」形态而不破坏统一观感。
