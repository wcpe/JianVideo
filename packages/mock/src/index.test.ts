import { describe, expect, it } from 'vitest';
import { findScenario } from './index';

describe('mock package', () => {
  it('能解析百万素材场景', () => {
    expect(findScenario('million-assets').dataset).toBe('target-1m');
  });

  it('未知场景抛出中文错误', () => {
    expect(() => findScenario('ai-review-pending')).toThrow('未知 mock 场景');
  });
});
