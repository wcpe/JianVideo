# 功能规格：可收缩左侧导航

> 状态：开发中　·　关联 PRD：FR-54　·　分支：main

## 1. 背景与目标

桌面端左侧导航栏当前固定宽 180px（图标 + 文字），占用横向空间。本变更让桌面端 navbar 支持「收缩为图标态 ↔ 展开」切换，收缩态仅显示图标（hover 出 Tooltip 保可用性），状态持久化到 localStorage 刷新后保持，给用户更大的内容显示区。属于第六期（界面与运维完善）。

移动端（< sm 断点）的抽屉（Drawer）+ 汉堡导航行为**完全不变**，本 FR 只影响桌面端 navbar。

## 2. 需求（要什么）

- 范围内：
  - 桌面端 navbar 支持收缩/展开切换：展开 180px（图标 + 文字），收缩约 64px（仅图标）。
  - 收缩态下导航项仅显示图标，外裹 Mantine `Tooltip`（label = 导航名）以保可用性。
  - navbar 内放一个切换按钮，点击在收缩/展开间切换。
  - 收缩/展开状态持久化到 localStorage（key `jianvideo-nav-collapsed`），刷新后保持。
- 不做（范围外，属后续 FR，禁止提前）：
  - 设置内容补齐（FR-56）、页脚/开源协议页（FR-57）、页眉更新提示（FR-58）。
  - 不改导航项本身（顺序/路由/图标/数量），不改各业务页面。
  - 不引新依赖（Mantine `Tooltip` `AppShell` 受控 width、`@tabler/icons-react` 均已在用）。

## 3. 设计（怎么做）

- 纯前端，无后端 / API / 数据模型改动，不涉及架构决策，无新 ADR。
- 抽一个小 hook `useNavCollapsed`（`frontend/src/hooks/useNavCollapsed.ts`）：管理收缩布尔状态与 localStorage 持久化（key `jianvideo-nav-collapsed`，值 `'1'` 表收缩），暴露 `[collapsed, toggle]`。读取时容错（无值/异常默认展开）。
- `AppLayout`：
  - `AppShell` 的 `navbar={{ width: collapsed ? 64 : 180, breakpoint: 'sm' }}` 随状态变（Mantine 既有受控能力）。
  - `renderNavLink` 增 `collapsed` 形参：收缩态只渲染图标、外裹 `Tooltip`（`label` 导航名、`position="right"`），并去掉文字 `Text`；展开态维持现状（图标 + 文字）。仅桌面 Navbar 受 `collapsed` 影响，Drawer 内固定展开渲染（移动端不变）。
  - navbar 底部放切换 `ActionIcon`（图标 `IconLayoutSidebarLeftCollapse` / `IconLayoutSidebarLeftExpand`），`aria-label` 区分「收起导航」「展开导航」，点击调 `toggle`。
  - 桌面 `AppShell.Navbar` 加 `data-collapsed={collapsed}` 作为可断言信号。

## 4. 任务拆分

- [ ] 复制模板写本 spec；PRD FR-54 状态「计划」→「开发中」
- [ ] 测试先行：扩充 `AppLayout.test.tsx`（默认展开图标+文字可见 / 点切换→收缩态文字隐藏+Tooltip+navbar data-collapsed / localStorage 写入 / 再 mount 读取恢复 / 移动端 Drawer 汉堡回归）
- [ ] 实现 `useNavCollapsed` hook + `AppLayout` 收缩态（受控 width、收缩仅图标 + Tooltip、切换按钮）
- [ ] 文档同步：PRD 状态、CHANGELOG 未发布段「新增」

## 5. 验收标准

- 扩充后的 `AppLayout.test.tsx` 全绿：
  - 默认（localStorage 无值）展开，导航名文字（如「时间轴」）可见。
  - 点切换按钮后进入收缩态：导航名文字不再直接可见、切换按钮 aria 切换、桌面 navbar `data-collapsed="true"`。
  - 收缩后 localStorage `jianvideo-nav-collapsed` 被写入收缩值。
  - 预置 localStorage 为收缩值再 mount，初始即为收缩态（恢复）。
  - 移动端汉堡按钮（`aria-label="导航菜单"`）等现有断言保持绿。
- 现有 `AppLayout.test.tsx` 主题切换等用例保持绿。
- `npx tsc --noEmit` 与 `npx vitest run` 全量绿；eslint 改动文件无新增告警。
- 手动复验（交由用户）：浏览器点收缩→仅图标 + hover 出 Tooltip，点展开→恢复图标 + 文字，刷新后保持上次状态；移动端（< sm）抽屉 + 汉堡行为不变。

## 6. 风险 / 待定

- 低风险。需保证收缩态对移动端 Drawer 零影响——通过「Drawer 内 renderNavLink 固定传展开」隔离。
- jsdom 下 Tooltip 仅 hover 才渲染 label，测试以「文字不在文档中」+ `data-collapsed` 断言收缩态，不依赖 hover。
