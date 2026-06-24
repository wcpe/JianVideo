# FR-92 设计系统 token 基础

## 需求（WHAT / WHY）
两轮设计走查暴露：间距/圆角/阴影/字号散落各处、无统一刻度，卡片扁平无层次，H1 过大，焦点环弱。FR-92 在 Mantine 主题集中定义/调优设计 token，并设全局默认，使全站经 Mantine **级联**统一——是第九期其余界面 FR（FR-93~107）的依赖地基。

## 设计（HOW）
**不发明新 token 体系**（Mantine 已有 `theme.spacing/radius/shadows/fontSizes/lineHeights` 与 `--mantine-*` CSS 变量，代码已大量在用），**不做全站 find-replace**（违精准修改）。改为在 `frontend/src/theme.ts` 的 `appTheme = createTheme({...})` 集中：

- `defaultRadius: 'md'`——所有组件（卡片/按钮/输入）统一继承圆角，免逐处写死。
- `focusRing: 'auto'`——可见焦点环，无障碍地基（FR-97 依赖）。
- `cursorType: 'pointer'`——可点控件（Select/Checkbox 等）光标为手型。
- `radius`：显式圆角刻度（xs→xl）。
- `shadows`：精炼的 elevation 阴影刻度（消解"卡片扁平无层次"）。
- `headings.sizes`：平衡的标题刻度（收小 H1，消解"H1 过大"）。
- `lineHeights`：略放宽行高，提升正文可读性。
- `other`：自定义语义 token（如 `contentMaxWidth` 供后续布局 FR 限宽用）。

`spacing` / `fontSizes` 沿用 Mantine 默认（已合理）以避免大面积布局回归；其值仍由 Mantine 暴露为 token，组件继续用 `p="md"`/`gap="sm"` 等。`themeCssVariablesResolver`（FR-84 的 dimmed 覆盖）保留。

## 任务
1. 扩展 `frontend/src/theme.ts` 的 `appTheme`。
2. 守护测试 `frontend/src/design-tokens.test.ts`：断言 `appTheme` 暴露上述 token。
3. doc-sync：ARCHITECTURE 设计系统说明、CHANGELOG。

## 验收
- `appTheme` 暴露 `defaultRadius`/`focusRing`/`radius`/`shadows`/`headings`/`lineHeights`/`other.contentMaxWidth`；守护测试绿。
- `npm run build`(tsc -b)+ `npm run test`(vitest) 全绿；既有主题守护测试（theme-transition-contrast / theme-dark-fallback / theme-bg-token）不回归。
- 【真机】抽查 3 页（时间轴/设置/播放）暗亮双主题视觉无破版：圆角/标题/焦点环统一，卡片有层次。
