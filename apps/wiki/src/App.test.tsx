import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { App } from './App';

const reactActGlobal = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean };
reactActGlobal.IS_REACT_ACT_ENVIRONMENT = true;

describe('wiki app', () => {
  it('渲染 wiki 博物馆入口', () => {
    const html = renderToString(<App />);

    expect(html).toContain('JianVideo Wiki');
    expect(html).toContain('媒体卡片');
    expect(html).toContain('百万素材压力场景');
  });

  it('支持搜索、分组、场景切换与状态预览', () => {
    const container = document.createElement('div');
    const root = createRoot(container);

    act(() => {
      root.render(<App />);
    });

    const search = requireInput(container, '[data-testid="wiki-search"]');

    act(() => {
      search.value = 'HLS';
      search.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="wiki-preview-hls-preview-card"]')?.textContent).toContain('HLS');

    act(() => {
      requireButton(container, '[data-testid="wiki-group-task"]').click();
    });
    expect(container.querySelector('[data-testid="wiki-preview-task-status"]')?.textContent).toContain('转码任务');

    const scenario = requireSelect(container, '[data-testid="wiki-scenario-select"]');
    act(() => {
      scenario.value = 'transcode-failed';
      scenario.dispatchEvent(new Event('change', { bubbles: true }));
    });
    expect(container.querySelector('[data-testid="wiki-selected-scenario"]')?.textContent).toContain('转码失败');

    act(() => {
      requireButton(container, '[data-testid="wiki-state-error"]').click();
    });
    expect(container.querySelector('[data-testid="wiki-active-state"]')?.textContent).toContain('error');
    expect(container.querySelector('[data-testid="wiki-snippet-task-status"]')?.textContent).toContain('@jianvideo/ui');

    act(() => {
      root.unmount();
    });
  });
});

function requireInput(container: Element, selector: string): HTMLInputElement {
  const element = requireElement(container, selector);
  if (!(element instanceof HTMLInputElement)) {
    throw new Error(`测试元素不是输入框：${selector}`);
  }
  return element;
}

function requireSelect(container: Element, selector: string): HTMLSelectElement {
  const element = requireElement(container, selector);
  if (!(element instanceof HTMLSelectElement)) {
    throw new Error(`测试元素不是下拉框：${selector}`);
  }
  return element;
}

function requireButton(container: Element, selector: string): HTMLButtonElement {
  const element = requireElement(container, selector);
  if (!(element instanceof HTMLButtonElement)) {
    throw new Error(`测试元素不是按钮：${selector}`);
  }
  return element;
}

function requireElement(container: Element, selector: string): Element {
  const element = container.querySelector(selector);
  if (!element) {
    throw new Error(`缺少测试元素：${selector}`);
  }
  return element;
}
