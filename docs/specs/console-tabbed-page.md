# 功能规格：系统信息与设置合并为单页 + 顶部 tab

> 状态：开发中　·　关联 PRD：FR-55　·　分支：main

## 1. 背景与目标

当前「系统信息」（`/system`）与「设置」（`/settings`）是两个独立页面、两个独立左侧导航项。参考宝塔面板，把两者合并为同一页内的两个顶部 tab，左侧导航只保留一个「系统」入口，降低导航项数量、把同类运维内容收拢一处。属于 P6（控制台体验完善）。

本变更是**纯前端重组，行为不变**：系统信息页与设置页的全部既有内容、交互一个都不少，只是改变它们的承载方式（从两条路由两个导航项，变为一页两 tab）。

## 2. 需求（要什么）

- 范围内：
  - 新建统一页 `ConsolePage`，用 Mantine `Tabs` 承载两个 tab：「系统信息」渲染 `<SystemPage/>`、「设置」渲染 `<SettingsPage/>`（原组件原样作 tab 内容，不改其实现）。
  - tab 状态以 URL query 控制（`useSearchParams`，`?tab=system|settings`，缺省 `system`），切 tab 同步更新 query，便于深链（FR-58 后续跳转到系统信息 tab 用）。
  - 路由：`/system` → `ConsolePage`；`/settings` → 重定向到 `/system?tab=settings`（`<Navigate replace />`），旧两路由都可达不留死链。
  - 导航：`AppLayout` 把 `/system`「系统信息」与 `/settings`「设置」两导航项合并为一个「系统」（路由 `/system`），导航项总数减一。
- 不做（范围外，属后续 FR，禁止提前）：
  - 环境变量查看 / ffmpeg 路径配置（FR-56）
  - 收缩导航（FR-54）
  - 页脚 / 开源协议页（FR-57）
  - 页眉更新提示（FR-58）
  - 不新增任何设置内容、不改 SystemPage / SettingsPage 既有行为。

## 3. 设计（怎么做）

- 纯前端重组，无后端 / API / 数据模型改动，无新增依赖（Mantine `Tabs` 与 `react-router-dom` 的 `useSearchParams` 均已在用）。不涉及架构决策，无新 ADR。
- `ConsolePage` 读 `searchParams.get('tab')`，非 `settings` 一律视作 `system`（缺省与非法值都落回系统信息 tab）。切 tab 时 `setSearchParams({ tab })` 更新 query。
- `/settings` 走 `App.tsx` 的 `<Navigate to="/system?tab=settings" replace />`，保留旧链可达性、不在历史里留多余条目。
- 保留 `SystemPage` / `SettingsPage` 作为 tab 内容，免改它们的现有测试。两页页面级 `<Title>`（「系统诊断」「设置」）与 tab 标签语义重复但不冲突，**保留不动**以确保旧测试断言（如 `screen.getByText('系统诊断')`）继续通过。

## 4. 任务拆分

- [ ] 复制模板写本 spec；PRD FR-55 状态「计划」→「开发中」
- [ ] 测试先行：新增 `ConsolePage.test.tsx`（两 tab 渲染 / 默认 system / 切换 settings + query / `/settings` 入口定位 settings）
- [ ] 实现 `ConsolePage.tsx`；`App.tsx` 路由改造（`/system`→ConsolePage、`/settings`→重定向）；`AppLayout.tsx` 合并导航项
- [ ] 文档同步：PRD 状态、ARCHITECTURE（`/system`、`/settings` 描述）、CHANGELOG 未发布段「新增」

## 5. 验收标准

- `ConsolePage.test.tsx` 全绿：渲染出「系统信息」「设置」两个 tab；默认处于系统信息 tab 且可见其内容；点「设置」tab 切换后 URL query 变为 `tab=settings` 且看到设置项（扫描周期 / 回收站）；以 `/settings` 进入时定位到设置 tab。
- 现有 `SystemPage.test.tsx`（10 用例）、`SettingsPage.test.tsx`、`AppLayout.test.tsx` 保持绿（组件未改天然通过；导航测试不依赖具体导航项数量）。
- `npx tsc --noEmit` 与 `npx vitest run` 全量绿；eslint 改动文件无新增告警。
- 手动复验（交由用户）：`/system` 显示两 tab、内容与改造前一致；`/settings` 跳转到设置 tab；左侧导航只剩一个「系统」入口。

## 6. 风险 / 待定

- 低风险。唯一需保证的是不破坏 SystemPage / SettingsPage 既有测试断言——通过「保留原组件原样、保留页面级标题」规避。
