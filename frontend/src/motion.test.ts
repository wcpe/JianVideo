import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';

// FR-96 守护（index.css 规则层面）：
// 1) 可点表面（卡片/按钮/导航）有 hover 平滑过渡与抬升，且不用 transition: all；
// 2) 路由切换有渐入动画、顶部进度条有相关 keyframes；
// 3) prefers-reduced-motion: reduce 下 hover 抬升 / 路由动画均被禁用（无障碍兜底）。

const css = readFileSync(path.join(path.resolve(__dirname), 'index.css'), 'utf-8');

describe('FR-96 可点表面 hover 过渡与抬升', () => {
  it('卡片/按钮/导航项加过渡，且不使用 transition: all（避免大面积重绘）', () => {
    expect(css).toMatch(/\.mantine-Card-root[\s\S]*?transition:/);
    expect(css).toMatch(/\.mantine-Button-root/);
    expect(css).toMatch(/\.mantine-NavLink-root/);
    expect(css).not.toMatch(/transition:\s*all/);
  });

  it('卡片 hover 上移并加深阴影（复用 FR-92 阴影 token）', () => {
    expect(css).toMatch(/\.mantine-Card-root:hover\s*\{[\s\S]*?transform:\s*translateY/);
    expect(css).toMatch(
      /\.mantine-Card-root:hover\s*\{[\s\S]*?box-shadow:\s*var\(--mantine-shadow-/,
    );
  });
});

describe('FR-96 路由切换动画与顶部进度条', () => {
  it('路由切换有渐入动画类与 keyframes', () => {
    expect(css).toMatch(/\.route-fade\s*\{[\s\S]*?animation:/);
    expect(css).toMatch(/@keyframes\s+jianvideo-route-fade-in/);
  });

  it('定义顶部进度条相关 keyframes', () => {
    expect(css).toMatch(/@keyframes\s+jianvideo-topbar-/);
  });
});

describe('FR-96 prefers-reduced-motion 无障碍兜底', () => {
  it('reduce 媒体查询下禁用 hover 抬升与路由动画', () => {
    const idx = css.indexOf('prefers-reduced-motion');
    expect(idx).toBeGreaterThanOrEqual(0);
    const reduceBlock = css.slice(idx);
    // hover 抬升被关闭
    expect(reduceBlock).toMatch(/\.mantine-Card-root:hover\s*\{[\s\S]*?transform:\s*none/);
    // 可点表面过渡被关闭
    expect(reduceBlock).toMatch(/\.mantine-NavLink-root[\s\S]*?transition:\s*none/);
    // 路由渐入动画被关闭
    expect(reduceBlock).toMatch(/\.route-fade\s*\{[\s\S]*?animation:\s*none/);
  });
});
