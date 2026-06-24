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

// 应用主题：当前仅承载 dimmed 对比度覆盖，其余沿用 Mantine 默认。
export const appTheme = createTheme({})
