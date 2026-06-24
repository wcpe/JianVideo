# 功能规格：主题切换过渡与 dimmed 对比度达标

> 状态：开发中　·　关联 PRD：FR-84　·　分支：feature/fr-84-theme-transition

## 1. 背景与目标

项目支持亮 / 暗主题切换（FR-G），但当前存在两处体验缺陷，属第八期（P8）界面打磨：

- **硬切**：`frontend/src/index.css` 的 body 背景用 `var(--mantine-color-body)` 即时重算，切主题时背景 / 文字颜色瞬时跳变，观感生硬。
- **次要文案对比度偏低**：大量 `c="dimmed"` 次要文案在暗色下用 Mantine 默认 `dimmed`（暗色映射到 `dark.2`），叠在 `dark.7` / `dark.6` 表面上对比度仅约 4.0 / 3.5:1，处于 WCAG AA 边缘（正常文本要求 ≥4.5:1）。

目标：切主题平滑过渡；暗色下次要文案对比度达 WCAG AA。扩 FR-G。

## 2. 需求（要什么）

- 亮暗切换时，body（及大面积表面容器）的背景 / 文字颜色加约 120–180ms 过渡，平滑切换。
- 必须带 `prefers-reduced-motion: reduce` 兜底，无障碍场景禁用过渡。
- 避免 `transition: all`，仅针对 body 与少量大面积表面，防性能与闪烁。
- 全局上调暗色 `dimmed` 次要文案亮度，使暗色表面（dark.7 / dark.6）上：正常文本 ≥4.5:1、大字（≥18px 或粗体）≥3:1。
- 一处改、全局生效，不逐处替换 94 个站点；保持语义变量（不引入写死颜色 token）。
- 范围内：`frontend/src/index.css` 过渡规则、`frontend/src/App.tsx` 通过 `createTheme` + `cssVariablesResolver` 上调 dimmed。
- 不做（范围外）：逐处替换 `c="dimmed"`；亮色模式 dimmed 调整（亮色 gray.6 已达标）；任何后端改动；引入新依赖。

## 3. 设计（怎么做）

- **过渡（index.css）**：给 body 加 `transition: background-color 150ms ease, color 150ms ease;`。在 `@media (prefers-reduced-motion: reduce)` 内将 body 的 `transition` 置为 `none`。仅限指定属性，不用 `transition: all`。
- **对比度（App.tsx）**：用 `createTheme` 创建主题，配 `cssVariablesResolver`，在 `dark` 变体下把 `--mantine-color-dimmed` 覆盖为更亮的语义变量 `var(--mantine-color-dark-1)`（#b8b8b8）；亮色保持 Mantine 默认（gray.6，已达标）。保持语义变量，不写死 hex。
  - 对比度验算（Mantine 7 默认暗色调色板）：dark.1(#b8b8b8) 叠 dark.7(#242424) = 7.83:1、叠 dark.6(#2e2e2e) = 6.85:1，均 ≥4.5:1。原 dark.2 为 4.04 / 3.53:1。
- 无新 ADR：未改技术栈 / 架构 / 依赖方向，仅前端主题配置与样式微调。

## 4. 任务拆分

- [x] 复制规格模板、PRD FR-84 计划→开发中
- [x] 测试先行：断言 index.css 过渡规则与 reduced-motion 兜底存在、断言 cssVariablesResolver 暗色 dimmed 上调
- [x] 实现 index.css 过渡 + reduced-motion 兜底
- [x] 实现 App.tsx createTheme + cssVariablesResolver 上调暗色 dimmed
- [x] 文档同步：PRD 状态、CHANGELOG 未发布段末尾追加
- [x] 两守护测试（theme-dark-fallback / theme-bg-token）保持全绿

## 5. 验收标准

- `frontend/` 下 `npm run build`（tsc -b + vite build）通过。
- `frontend/` 下 `npm run test`（vitest）全绿，含本 FR 新增测试与两守护测试（theme-dark-fallback、theme-bg-token）。
- 暗色 dimmed 在 dark.7 / dark.6 上对比度 ≥4.5:1（自动化断言覆盖：对 cssVariablesResolver 输出的 dark 变体 dimmed 值断言为 dark.1，并以对比度计算验证达标）。
- index.css 存在 body 颜色过渡规则，且 `prefers-reduced-motion: reduce` 内禁用过渡（自动化断言源码存在性）。
- 真机维度（待真机验）：实机切主题观感平滑、开启系统「减少动态效果」后过渡被禁用、暗色下次要文案肉眼可辨清晰。

## 6. 风险 / 待定

- 真机过渡观感与「减少动态效果」生效需实机确认（待真机验）。
- cssVariablesResolver 仅覆盖 dimmed 单变量，不影响其他语义色，风险面小。
