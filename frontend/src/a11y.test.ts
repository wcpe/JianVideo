import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';

// FR-97 守护（index.css 规则层面）：
// 1) 键盘聚焦（:focus-visible）时可交互元素显示品牌紫焦点环，鼠标点击（非 focus-visible）不显；
// 2) 移动端（pointer: coarse）下图标按钮（ActionIcon）触控目标 ≥44px，避免误触；
// 这些是无障碍地基，纯 CSS 规则，故在 index.css 文本层面断言其存在与关键属性。

const css = readFileSync(path.join(path.resolve(__dirname), 'index.css'), 'utf-8');

describe('FR-97 键盘焦点可见环', () => {
  it('用 :focus-visible 给可交互元素加焦点环（按钮/链接/导航/输入）', () => {
    expect(css).toMatch(/:focus-visible\s*\{[\s\S]*?outline:/);
    // 覆盖典型可交互元素
    expect(css).toMatch(/button[\s\S]*?:focus-visible|:focus-visible[\s\S]*?button/);
    expect(css).toMatch(/a:focus-visible|a[\s\S]*?:focus-visible/);
  });

  it('焦点环复用 FR-93 品牌紫主色 token（不写死 hex）', () => {
    const idx = css.indexOf(':focus-visible');
    expect(idx).toBeGreaterThanOrEqual(0);
    const block = css.slice(idx);
    expect(block).toMatch(/outline:[^;]*var\(--mantine-color-purple-/);
  });

  it('鼠标点击（非 focus-visible）不残留焦点环：去掉 :focus 默认 outline', () => {
    expect(css).toMatch(/:focus:not\(:focus-visible\)\s*\{[\s\S]*?outline:\s*none/);
  });
});

describe('FR-97 移动端触控目标 ≥44px', () => {
  it('在 pointer: coarse 媒体查询下给 ActionIcon 设最小 44px 触控区', () => {
    const idx = css.indexOf('pointer: coarse');
    expect(idx).toBeGreaterThanOrEqual(0);
    const block = css.slice(idx);
    expect(block).toMatch(/\.mantine-ActionIcon-root\s*\{[\s\S]*?min-(width|height):\s*44px/);
    expect(block).toMatch(/\.mantine-ActionIcon-root\s*\{[\s\S]*?min-height:\s*44px/);
  });
});
