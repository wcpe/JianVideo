import { describe, expect, it } from 'vitest';
import { listWikiScenarioTitles, wikiPreviewCatalog } from './catalog';

describe('wiki catalog', () => {
  it('组件代码片段只指向 packages 导入路径', () => {
    expect(wikiPreviewCatalog.every((item) => item.snippet.importPath.startsWith('@jianvideo/'))).toBe(true);
  });

  it('展示共享 mock 场景', () => {
    expect(listWikiScenarioTitles()).toContain('百万素材压力场景');
  });

  it('登记 API client 示例', () => {
    expect(wikiPreviewCatalog.map((item) => item.id)).toContain('api-client-demo');
  });
});
