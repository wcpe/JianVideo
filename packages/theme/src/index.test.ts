import { describe, expect, it } from 'vitest';
import { resolveDensity, themeTokens } from './index';

describe('theme package', () => {
  it('触控设备使用舒适密度', () => {
    expect(resolveDensity({ density: 'compact', pointer: 'coarse' })).toBe('comfortable');
  });

  it('精确指针设备保留请求密度', () => {
    expect(resolveDensity({ density: 'compact', pointer: 'fine' })).toBe('compact');
  });

  it('暴露稳定的主题 token', () => {
    expect(themeTokens.radiusSm).toBe('6px');
  });
});
