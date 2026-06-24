# 功能规格：空态/加载/反馈统一（扩 FR-88）

> 状态：开发中　·　关联 PRD：FR-98　·　分支：feature/fr-98-empty-unify

## 1. 背景与目标

第九期界面打磨。FR-88 已落地通用 `EmptyState` 组件（居中原创插画 + 文案 + 可选 CTA），并接入统计 / 转码 / 巡检 / 重复项 / 回收站等页。仍有若干页面停留在「单行灰字」空态、首屏加载直接空白闪现、通知样式各页不一。本 FR 把这些观感统一到 FR-88 已有的设计语言，降低「看起来没加载出来 / 不知道下一步做什么」的困惑。属界面优化专项（P9）。

## 2. 需求（要什么）

范围内：
- **EmptyState 扩覆盖**：把仍是单行灰字的空态接入 `EmptyState`——
  - 照片地图页（`MapPage`）：无带 GPS 照片时；
  - 相册页（`AlbumsPage`）：无相册时、相册详情无媒体时；
  - 时间轴 / 目录浏览：**搜索/筛选无结果**态——与「空库 / 空目录」区分，给「无匹配结果 + 清除筛选」引导。
- **列表骨架屏**：首屏加载用 `Skeleton` 占位替代空白闪现（至少相册列表、相册详情；时间轴 / 目录已有骨架，保持）。
- **通知/错误态一致**：抽出 `notify` 助手统一成功/失败通知的颜色 / 图标 / autoClose 约定；错误态（加载失败）给「插画 + 重试」（复用 `EmptyState`，至少 `MapPage`、`AlbumsPage`）。

不做（范围外）：
- 不重排页面布局、不动业务逻辑 / 请求参数。
- 不引第三方插画库、无新依赖。
- 不对全仓 19 处 `notifications.show` 做无关大规模翻新——仅本 FR 触碰的页面接入 `notify` 助手，确立约定；其余按既有风格保留（精准修改）。
- AlbumsPage 既有面包屑（FR-95）保留，仅在其上叠空态。

## 3. 设计（怎么做）

无新 ADR（纯前端观感统一，不涉架构决策）。

- `notify`（新增 `frontend/src/utils/notify.ts`）：薄封装 `@mantine/notifications`，导出 `notifySuccess` / `notifyError`，固定颜色（绿/红）、图标（√/×）、autoClose（成功 2500ms、失败 4000ms）。
- `EmptyState`：复用现有组件；无新插画需求（无 GPS / 空相册 / 无结果 / 加载失败均用内置空盒插画或 tabler 图标作 `icon`）。
- `TimelineView` / `DirectoryBrowser`：新增可选 `filtered?: boolean` 与 `onClearFilter?: () => void`。当 `filtered` 且 0 结果时渲染「无匹配结果」EmptyState + 「清除筛选」CTA；否则渲染原「空库 / 空目录」EmptyState。父页（`TimelinePage` / `BrowsePage`）按是否有筛选条件传入 `filtered` 并提供清除回调。
- `MapPage` / `AlbumsPage`：空态换 `EmptyState`；加载态用 `Skeleton`；加载失败用 `EmptyState`（错误图标 + 重试 CTA）。

## 4. 任务拆分

- [x] `notify` 助手 + 单测
- [x] `MapPage`：无 GPS 空态 / 加载失败态换 `EmptyState`，加载态 `Skeleton`
- [x] `AlbumsPage`：空相册 / 空相册详情换 `EmptyState`，加载态 `Skeleton`，加载失败重试态
- [x] `TimelineView` / `DirectoryBrowser`：空库 vs 无结果区分 + 「清除筛选」CTA
- [x] `TimelinePage` / `BrowsePage`：传 `filtered` 与清除回调
- [x] 测试：MapPage 无 GPS → EmptyState；空相册 → EmptyState；有筛选且 0 结果 → 无结果态；加载态 → Skeleton；既有测试全绿
- [x] 文档同步：PRD 状态、CHANGELOG

## 5. 验收标准

- `frontend/` 下 `npm run build`（tsc -b）与 `npm run test`（vitest）全绿。
- 新增测试覆盖：MapPage 无 GPS 渲染 EmptyState、空相册渲染 EmptyState、有筛选且 0 结果渲染「无结果」态、首屏加载渲染 Skeleton。
- 既有测试（含 MapPage「暂无带 GPS 定位的照片」文案断言）保持通过。
- 真机（待用户确认）：各空态 / 加载态在亮暗主题下居中、无大片留白、无结果态有「清除筛选」引导——标「待真机验」。

## 6. 风险 / 待定

- MapPage 既有测试断言空态文案含「暂无带 GPS 定位的照片」，EmptyState 文案须保留该片段，避免回归。
- 通知助手仅在本 FR 触碰页面接入，全仓统一留后续；不在本 FR 扩大范围。
