import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';

// 页眉宽度不随全局滚动条变化（CSS 守护）：
// 页面滚动容器为 html/window，html 上 scrollbar-gutter:stable 让「普通流」的内容区（AppShell.Main）
// 右缘始终让开滚动条槽位；但 AppShell 的页眉/页脚是 position:fixed、inset-inline:0，相对视口（含滚动条区）
// 铺满，宽度比内容区多出一个滚动条槽位，切页出/隐滚动条时表现为页眉右侧内容相对内容区偏移/跳动。
// 修复：给固定定位的页眉/页脚同样让开该槽位——用「100vw - AppShell 根宽度」算出滚动条槽宽存为变量，
// 页眉/页脚据其做右侧内偏，使其右缘与内容区对齐、不随滚动条有无跳动。
// scrollbar-gutter / 100vw 属布局级行为、jsdom 不计算，故在 index.css 文本层面守护该机制。

const css = readFileSync(path.join(path.resolve(__dirname), 'index.css'), 'utf-8');

describe('页眉宽度不随全局滚动条变化（CSS 守护）', () => {
  it('AppShell 根元素据 100vw 与自身宽度算出滚动条槽位宽度变量', () => {
    // 在 AppShell 根（普通流、宽度=内容区不含滚动条）上，用 100vw（含滚动条视口宽）减自身 100% 得槽宽
    const idx = css.indexOf('.mantine-AppShell-root');
    expect(idx).toBeGreaterThanOrEqual(0);
    const block = css.slice(idx, css.indexOf('}', idx));
    expect(block).toMatch(/--app-scrollbar-gutter-width:\s*calc\(\s*100vw\s*-\s*100%\s*\)/);
  });

  it('固定定位的页眉/页脚右缘让开该槽位，与内容区对齐', () => {
    // 页眉与页脚以 inset-inline-end 让出滚动条槽位宽度，右缘与 AppShell.Main 内容区对齐
    const idx = css.indexOf('.mantine-AppShell-header,');
    expect(idx).toBeGreaterThanOrEqual(0);
    const block = css.slice(idx, css.indexOf('}', idx));
    expect(block).toMatch(/inset-inline-end:\s*var\(--app-scrollbar-gutter-width/);
  });

  it('播放页全屏沉浸态无滚动条，槽位宽度复位为 0，页眉/页脚不再内偏', () => {
    // 沉浸态已 overflow:hidden 无滚动条（且 scrollbar-gutter 复位为 auto），槽宽应为 0，避免右侧留空
    const idx = css.indexOf('body.play-immersive .mantine-AppShell-root');
    expect(idx).toBeGreaterThanOrEqual(0);
    const block = css.slice(idx, css.indexOf('}', idx));
    expect(block).toMatch(/--app-scrollbar-gutter-width:\s*0/);
  });
});
