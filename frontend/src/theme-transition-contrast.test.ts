import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import path from 'path'
import { DEFAULT_THEME } from '@mantine/core'
import { themeCssVariablesResolver } from './theme'

// FR-84 守护：主题切换过渡 + dimmed 对比度达标。
// 1) cssVariablesResolver 在暗色下把 dimmed 上调为更亮的语义变量，使暗色表面对比度达 WCAG AA；
// 2) index.css 给 body 加颜色过渡，并在 prefers-reduced-motion: reduce 下禁用（无障碍兜底），不用 transition: all。

const SRC = path.resolve(__dirname)

// Mantine 7 默认暗色调色板（用于对比度验算）
const DARK = {
  0: '#c9c9c9',
  1: '#b8b8b8',
  2: '#828282',
  6: '#2e2e2e',
  7: '#242424',
} as const

// 相对亮度（WCAG）
function luminance(hex: string): number {
  const c = hex.replace('#', '')
  const toLin = (v: number) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
  }
  const r = toLin(parseInt(c.slice(0, 2), 16))
  const g = toLin(parseInt(c.slice(2, 4), 16))
  const b = toLin(parseInt(c.slice(4, 6), 16))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

// 对比度比值（WCAG）
function contrast(fg: string, bg: string): number {
  const l1 = luminance(fg)
  const l2 = luminance(bg)
  const hi = Math.max(l1, l2)
  const lo = Math.min(l1, l2)
  return (hi + 0.05) / (lo + 0.05)
}

describe('FR-84 暗色 dimmed 对比度达 WCAG AA', () => {
  it('cssVariablesResolver 在暗色把 dimmed 上调为更亮的语义变量（保持语义、不写死 hex）', () => {
    const vars = themeCssVariablesResolver(DEFAULT_THEME)
    const dimmed = vars.dark['--mantine-color-dimmed']
    // 必须是更亮的语义变量 dark.1，不得是默认 dark.2，也不得写死 hex
    expect(dimmed).toBe('var(--mantine-color-dark-1)')
    expect(dimmed).not.toContain('#')
    // 亮色：默认 gray.6(#868e96) 落白底仅 3.32:1 不达 AA（FR-97 修复），
    // 下调为更深的语义变量 gray.7，不得写死 hex（详尽对比度校验见 theme.test.ts）
    const lightDimmed = vars.light['--mantine-color-dimmed']
    expect(lightDimmed).toBe('var(--mantine-color-gray-7)')
    expect(lightDimmed).not.toContain('#')
  })

  it('上调后的 dimmed 叠在 dark.7/dark.6 表面上正常文本对比度 ≥4.5:1', () => {
    // 解析 resolver 选用的语义变量到具体色值
    const dimmedHex = DARK[1]
    expect(contrast(dimmedHex, DARK[7])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(dimmedHex, DARK[6])).toBeGreaterThanOrEqual(4.5)
    // 反向佐证：原默认 dark.2 在 dark.6 上确实不达标（<4.5），证明此修复必要
    expect(contrast(DARK[2], DARK[6])).toBeLessThan(4.5)
  })
})

describe('FR-84 主题切换颜色过渡与无障碍兜底', () => {
  const css = readFileSync(path.join(SRC, 'index.css'), 'utf-8')

  it('body 加背景/文字颜色过渡（约 150ms），且不使用 transition: all', () => {
    // 仅针对 background-color / color，避免 transition: all 带来的性能/闪烁
    expect(css).toMatch(/transition:\s*background-color\s+\d+ms/)
    expect(css).toMatch(/color\s+\d+ms/)
    expect(css).not.toMatch(/transition:\s*all/)
  })

  it('prefers-reduced-motion: reduce 下禁用过渡（无障碍兜底）', () => {
    expect(css).toMatch(/@media\s*\(prefers-reduced-motion:\s*reduce\)/)
    // reduce 媒体查询块内应出现 transition: none 关闭过渡
    const idx = css.indexOf('prefers-reduced-motion')
    const tail = css.slice(idx)
    expect(tail).toMatch(/transition:\s*none/)
  })
})
