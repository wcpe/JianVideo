import { describe, expect, it } from 'vitest';
import { resolveDatasetSummary } from './summary';

describe('mock studio summary', () => {
  it('汇总共享 mock 数据集', () => {
    expect(resolveDatasetSummary()).toContain('百万素材压力场景:target-1m');
  });
});
