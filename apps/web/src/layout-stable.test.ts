import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';

// FR-133 守护（index.css 规则层面）：全局统一固定高度布局——通过 scrollbar-gutter: stable
// 始终为纵向滚动条预留固定宽度，使内容多（出滚动条）与内容少（无滚动条）的页面横向不偏移，
// 消除概览↔时间轴↔目录切页时的横向跳动。scrollbar-gutter 属于布局级行为、jsdom 不计算，
// 故在 index.css 文本层面守护该机制。

const css = readFileSync(path.join(path.resolve(__dirname), 'index.css'), 'utf-8');

describe('FR-133 全局统一固定高度布局（CSS 守护）', () => {
  it('html 始终预留滚动条宽度（scrollbar-gutter: stable），消除切页横向跳动', () => {
    // 取 html 选择器规则块，断言其内含 scrollbar-gutter: stable
    const idx = css.indexOf('html {');
    expect(idx).toBeGreaterThanOrEqual(0);
    const block = css.slice(idx, css.indexOf('}', idx));
    expect(block).toMatch(/scrollbar-gutter:\s*stable/);
  });

  it('播放页全屏沉浸态关闭滚动条预留（避免铺满视口下出现无用槽位）', () => {
    // 沉浸态已 overflow:hidden 无滚动条，需把 gutter 复位为 auto 以免右侧留空槽
    const idx = css.indexOf('body.play-immersive');
    expect(idx).toBeGreaterThanOrEqual(0);
    const block = css.slice(idx);
    expect(block).toMatch(/scrollbar-gutter:\s*auto/);
  });
});
