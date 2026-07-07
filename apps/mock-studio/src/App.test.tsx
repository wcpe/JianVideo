import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { App } from './App';

describe('mock studio app', () => {
  it('渲染 mock 数据集摘要', () => {
    const html = renderToString(<App />);

    expect(html).toContain('Mock Studio');
    expect(html).toContain('百万素材压力场景:target-1m');
  });
});
