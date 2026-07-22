import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';

// FR-131 守护（index.css 规则层面）：
// 1) 导航项更紧凑：.nav-link 设置更小的内边距（紧凑密度）；
// 2) 导航项有进入过渡动画（淡入），对应 keyframes 存在；
// 3) prefers-reduced-motion: reduce 下导航项进入动画被禁用（无障碍兜底）。

const css = readFileSync(path.join(path.resolve(__dirname), 'index.css'), 'utf-8');

describe('FR-131 导航紧凑化', () => {
  it('.nav-link 设置紧凑内边距（覆盖默认 p="xs"）', () => {
    // 紧凑密度：导航项规则块内显式写 padding（小于 Mantine xs 默认），收紧行高。
    // 用非贪婪匹配限定在首个 .nav-link { ... } 块内出现 padding，避免跨块误命中。
    expect(css).toMatch(/\.nav-link\s*\{[^}]*padding:\s*4px 8px/);
  });

  it('导航项有进入过渡动画与对应 keyframes', () => {
    expect(css).toMatch(/@keyframes\s+jianvideo-nav-item-in/);
    expect(css).toMatch(/\.nav-link\s*\{[\s\S]*?animation:/);
  });
});

describe('FR-131 prefers-reduced-motion 无障碍兜底', () => {
  it('reduce 下导航项进入动画被禁用', () => {
    const idx = css.indexOf('prefers-reduced-motion');
    expect(idx).toBeGreaterThanOrEqual(0);
    const reduceBlock = css.slice(idx);
    expect(reduceBlock).toMatch(/\.nav-link\s*\{[\s\S]*?animation:\s*none/);
  });
});
