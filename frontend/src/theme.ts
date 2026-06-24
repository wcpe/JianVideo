import { createTheme } from '@mantine/core'
import type { CSSVariablesResolver } from '@mantine/core'

// 全局 CSS 变量覆盖（FR-84）：
// Mantine 暗色默认把 `--mantine-color-dimmed` 映射到 dark.2(#828282)，
// 叠在 dark.7/dark.6 表面上对比度仅约 4.0/3.5:1，处于 WCAG AA 边缘。
// 这里在暗色变体下把 dimmed 上调为更亮的语义变量 dark.1(#b8b8b8)，
// 叠 dark.7=7.83:1、叠 dark.6=6.85:1，正常文本均 ≥4.5:1。
// 一处改、全局生效，所有 c="dimmed" 站点随之达标，且保持语义变量（不写死 hex）。
// 亮色保持 Mantine 默认（gray.6 已达标），故 light 不覆盖。
export const themeCssVariablesResolver: CSSVariablesResolver = () => ({
  variables: {},
  dark: {
    '--mantine-color-dimmed': 'var(--mantine-color-dark-1)',
  },
  light: {},
})

// 应用主题（FR-92 设计系统 token 地基）：集中定义/调优设计 token 并设全局默认，
// 经 Mantine 级联让全站圆角/层次/标题/焦点环统一，是第九期其余界面 FR 的依赖地基。
// 不发明新 token 体系（Mantine 已有 spacing/radius/shadows/fontSizes/lineHeights），
// 不做全站 find-replace；spacing/fontSizes 沿用 Mantine 默认以免大面积布局回归。
export const appTheme = createTheme({
  // 全局默认圆角：卡片/按钮/输入等统一继承，免逐处写死
  defaultRadius: 'md',
  // 可见焦点环（无障碍地基，FR-97 依赖）：键盘聚焦时显示
  focusRing: 'auto',
  // 可点控件（Select/Checkbox 等）光标为手型
  cursorType: 'pointer',
  // 圆角刻度 token
  radius: {
    xs: '4px',
    sm: '6px',
    md: '8px',
    lg: '12px',
    xl: '16px',
  },
  // 阴影 / 层次（elevation）刻度 token：消解「卡片扁平无层次」，给表面分级深度
  shadows: {
    xs: '0 1px 2px rgba(0, 0, 0, 0.06)',
    sm: '0 2px 6px rgba(0, 0, 0, 0.08)',
    md: '0 4px 12px rgba(0, 0, 0, 0.10)',
    lg: '0 8px 24px rgba(0, 0, 0, 0.12)',
    xl: '0 16px 40px rgba(0, 0, 0, 0.16)',
  },
  // 行高刻度：略放宽，提升正文可读性
  lineHeights: {
    xs: '1.4',
    sm: '1.45',
    md: '1.55',
    lg: '1.6',
    xl: '1.65',
  },
  // 标题刻度：平衡、收小 H1，消解「H1 过大过重」
  headings: {
    sizes: {
      h1: { fontSize: '1.75rem', lineHeight: '1.3', fontWeight: '700' },
      h2: { fontSize: '1.375rem', lineHeight: '1.35', fontWeight: '700' },
      h3: { fontSize: '1.125rem', lineHeight: '1.4', fontWeight: '600' },
      h4: { fontSize: '1rem', lineHeight: '1.45', fontWeight: '600' },
      h5: { fontSize: '0.875rem', lineHeight: '1.5', fontWeight: '600' },
      h6: { fontSize: '0.8125rem', lineHeight: '1.5', fontWeight: '600' },
    },
  },
  // 自定义语义 token：供后续布局 FR（如 FR-95 内容限宽居中）消费
  other: {
    contentMaxWidth: '1280px',
  },
})
