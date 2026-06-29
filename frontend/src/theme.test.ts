import { describe, it, expect } from 'vitest';
import { DEFAULT_THEME } from '@mantine/core';
import { themeCssVariablesResolver } from './theme';

// WCAG 相对亮度与对比度（纯函数），用于在测试中校验 dimmed 文字色达到 AA 4.5:1。
function luminance(hex: string): number {
  const c = hex.replace('#', '');
  const rgb = [0, 2, 4].map((i) => parseInt(c.slice(i, i + 2), 16) / 255);
  const lin = rgb.map((v) => (v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)));
  return 0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2];
}
function contrast(a: string, b: string): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

// Mantine 默认灰阶（用于把语义 token 选择与实际对比度绑定校验）
const GRAY_6 = '#868e96'; // 旧 light dimmed（落白底仅 3.32:1，AA 不达标）
const GRAY_7 = '#495057'; // 新 light dimmed 映射目标
const WHITE = '#ffffff';
const GRAY_1 = '#f3f4f5'; // 常见浅色卡面/徽标底

describe('themeCssVariablesResolver — dimmed 文字对比度（FR-97 修复）', () => {
  it('亮色把 --mantine-color-dimmed 映射到更深的 gray-7（语义 token，非写死 hex）', () => {
    const vars = themeCssVariablesResolver(DEFAULT_THEME);
    expect(vars.light['--mantine-color-dimmed']).toBe('var(--mantine-color-gray-7)');
  });

  it('暗色仍把 dimmed 上调为 dark-1（不回归 FR-84）', () => {
    const vars = themeCssVariablesResolver(DEFAULT_THEME);
    expect(vars.dark['--mantine-color-dimmed']).toBe('var(--mantine-color-dark-1)');
  });

  it('旧 gray-6 在白底/浅灰底均不达 AA 4.5:1（复现缺陷）', () => {
    expect(contrast(GRAY_6, WHITE)).toBeLessThan(4.5);
    expect(contrast(GRAY_6, GRAY_1)).toBeLessThan(4.5);
  });

  it('新 gray-7 在白底与浅灰底均达 AA 4.5:1（任意字号）', () => {
    expect(contrast(GRAY_7, WHITE)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(GRAY_7, GRAY_1)).toBeGreaterThanOrEqual(4.5);
  });
});
