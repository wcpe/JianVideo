import { describe, expect, it } from 'vitest';
import { createSnippet } from './index';

describe('ui package', () => {
  it('代码片段指向 packages 导入路径', () => {
    const snippet = createSnippet('MediaCardPreview', '@jianvideo/ui');

    expect(snippet.importPath).toBe('@jianvideo/ui');
    expect(snippet.code).toContain("from '@jianvideo/ui'");
  });
});
