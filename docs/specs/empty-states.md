# 功能规格：空态体验统一（FR-88）

> 状态：开发中　·　关联 PRD：FR-88（扩 FR-75/FR-77，覆盖 FR-70/FR-73）　·　分支：feature/fr-88-empty-states

## 1. 背景与目标

第八期界面体验打磨。当前多个列表/统计页在「无数据」或「少数据」时体验割裂：

- 统计页（FR-75）后端对空库/稀疏库返回非 null（total=0、数组空、热力固定 10 个 0），只有「加载失败」才落顶层空态。结果空库时仍逐卡渲染——续播热力渲成满是 0 的网格、时间线渲成孤立细条、进度概览卡 0/0，观感杂乱无引导。
- 转码（FR-77）、巡检（FR-73）、重复项（FR-70）、回收站（FR-25/26）页的空态仅一行 dimmed 文字，缺乏视觉锚点与一键行动入口。

目标：统一空态体验——居中插画/图标 + 标题 + 说明 + 可选 CTA，并让统计页在空/稀疏时隐藏无意义图表卡、改显整页引导。属第八期（P8）纯前端体验打磨。

## 2. 需求（要什么）

- 新增共享组件 `EmptyState`：居中原创 SVG 插画（或复用 tabler 图标作居中图标）+ 标题 + 说明文案 + 可选 CTA 按钮，全部 props 驱动。
- 统计页：`total=0` 显整页引导态（不渲染任何空图表卡）；「有媒体但 watched=0 且无续播无时间线」的稀疏态隐藏无意义卡（满 0 热力网格、孤立细条）。
- 转码/巡检/重复项页：空态用 `EmptyState` 显插画/图标 + 引导文案 + 可点 CTA，复用各页已有 handler（新建预设 / 开始巡检 / 扫描重复项）。
- 回收站页：空态用 `EmptyState`（无 CTA，仅插画 + 文案）。
- 范围内：纯前端展示层。
- 不做（范围外）：不碰后端（stats.go 对空/稀疏行为已正确）；不引第三方插画库/新依赖；不借机重排各页布局。

## 3. 设计（怎么做）

新增 `frontend/src/components/EmptyState.tsx`：

- props：`icon`（可选 ReactNode，默认内置原创 SVG 插画）、`title`、`description`（可选）、`action`（可选 `{ label, onClick, loading?, leftIcon? }`）。
- 布局：Mantine `<Stack align="center">` 居中，插画在上、标题、说明、CTA 按钮。深浅色皆可读——SVG 用 `currentColor` 跟随主题文本色、配合 Mantine dimmed。
- 内置原创 SVG 插画：简洁线性风格（空盒/空集合意象），`viewBox` 固定、`stroke="currentColor"`、半透明描边，不抄第三方插画库。

接入：

- StatsPage：`total === 0` → 整页 `EmptyState`（去媒体库引导，CTA 跳 `/library-manager`），提前 return、不渲任何图表卡。新增稀疏判定：`watched===0 && recent_timeline 全空 && position_heatmap 全 0` 时，隐藏续播热力卡与时间线卡（这两卡满 0 无意义），其余卡（进度概览、各库/格式、Top 榜）保持既有逐卡空守卫。
- TranscodePage：预设空 → `EmptyState`（CTA「新建预设」→ `openCreate`）；队列空 → `EmptyState`（无 CTA，文案引导去播放页加入预生成）。
- InspectPage：空态 → `EmptyState`（CTA「开始巡检」→ `handleScan`），文案区分「已巡检无问题」与「尚未巡检」。
- DuplicatesPage：空态 → `EmptyState`（CTA「扫描重复项」→ `handleScan`）。
- RecyclePage：空态 → `EmptyState`（无 CTA）。

无新数据模型、无新接口、无新依赖、无架构决策，故不新增 ADR。

## 4. 任务拆分

- [ ] 新增 `EmptyState` 组件 + 单测
- [ ] StatsPage 接入空/稀疏态 + 测试
- [ ] TranscodePage / InspectPage / DuplicatesPage / RecyclePage 接入空态 + 测试
- [ ] 文档同步：PRD 状态、CHANGELOG 未发布段

## 5. 验收标准

- `EmptyState` 单测：渲染插画/图标、标题、说明；传 action 时渲染按钮且点击触发回调、loading 禁用。
- StatsPage 测试：`total=0` 断言整页引导且不渲空图表卡（无 `heat-cell-0`）；「有媒体但 watched=0/recent_timeline=[]/position_heatmap 全 0」断言不出现满 0 热力网格/孤立细条。
- 转码/巡检/重复项页测试：空数据时渲染引导图标/文案与可点 CTA，点击触发对应 handler。
- 既有相关测试不回归。
- `frontend/` 下 `npm run build`(tsc -b) 与 `npm run test`(vitest) 全绿。
- 真机维度：各页空态观感（图标/文案/CTA 居中、无大片空白）——标「待真机验」（纯渲染逻辑，mock 已覆盖大部分）。

## 6. 风险 / 待定

- 稀疏态判定口径需与「逐卡空守卫」共存，避免重复隐藏或漏隐藏；以「整卡满 0 无意义」为隐藏边界。
- 原创 SVG 在深浅主题下的对比度靠 `currentColor` + 透明度，真机两主题各看一眼。
