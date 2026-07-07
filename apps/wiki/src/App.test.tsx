import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { App } from './App';

describe('wiki app', () => {
  it('渲染 wiki 博物馆入口', () => {
    const html = renderToString(<App />);

    expect(html).toContain('JianVideo Wiki');
    expect(html).toContain('媒体卡片');
    expect(html).toContain('百万素材压力场景');
  });
});
