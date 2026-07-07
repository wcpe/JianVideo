import { describe, expect, it } from 'vitest';
import { estimateTextureMemoryBytes, resolveVisibleWindow } from './index';

describe('render-pixi package', () => {
  it('可见窗口不会越界', () => {
    expect(resolveVisibleWindow(100, 2, 10, 5)).toEqual({ start: 0, end: 17 });
  });

  it('按 RGBA 估算纹理内存', () => {
    expect(estimateTextureMemoryBytes(2, 10, 10)).toBe(800);
  });
});
