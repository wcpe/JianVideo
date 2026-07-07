import { describe, expect, it } from 'vitest';
import { summarizeFrameCost } from './index';

describe('benchmark package', () => {
  it('计算帧耗时分位数', () => {
    expect(summarizeFrameCost([1, 2, 3, 4, 5])).toEqual({ p95: 5, p99: 5 });
  });

  it('空样本抛出中文错误', () => {
    expect(() => summarizeFrameCost([])).toThrow('无法计算空样本分位数');
  });
});
