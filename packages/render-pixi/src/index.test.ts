import { describe, expect, it, vi } from 'vitest';
import {
  TexturePool,
  createPixiPreviewCells,
  createRenderMetricsSnapshot,
  estimateTextureMemoryBytes,
  mountPixiGridPreview,
  resolveGridWindow,
  resolveVisibleWindow,
  shouldRequestHlsPreview,
} from './index';

vi.mock('pixi.js', () => {
  class FakeApplication {
    readonly canvas = { height: 0, width: 0 } as HTMLCanvasElement;
    readonly renderer = { name: 'webgl-test' };
    readonly stage = { addChild() {} };

    async init(): Promise<void> {}

    render(): void {}

    destroy(): void {}
  }

  class FakeGraphics {
    rect(): { fill: () => void } {
      return { fill() {} };
    }
  }

  return { Application: FakeApplication, Graphics: FakeGraphics, VERSION: 'mock-pixi' };
});

describe('render-pixi package', () => {
  it('可见窗口不会越界', () => {
    expect(resolveVisibleWindow(100, 2, 10, 5)).toEqual({ start: 0, end: 17 });
  });

  it('按 RGBA 估算纹理内存', () => {
    expect(estimateTextureMemoryBytes(2, 10, 10)).toBe(800);
  });

  it('按行计算网格可见窗口与 overscan', () => {
    expect(
      resolveGridWindow({
        total: 1_000_000,
        columns: 8,
        itemHeight: 120,
        scrollTop: 2_400,
        viewportHeight: 600,
        overscanRows: 2,
      }),
    ).toEqual({
      start: 144,
      end: 216,
      firstVisible: 160,
      visibleCount: 40,
    });
  });

  it('纹理池按最近最少使用淘汰并释放纹理', () => {
    const destroyed: string[] = [];
    const pool = new TexturePool({ maxTextures: 2, maxBytes: 1_000 });

    pool.put({
      key: 'a',
      width: 10,
      height: 10,
      destroy: () => {
        destroyed.push('a');
      },
    });
    pool.put({
      key: 'b',
      width: 10,
      height: 10,
      destroy: () => {
        destroyed.push('b');
      },
    });
    pool.get('a');
    pool.put({
      key: 'c',
      width: 10,
      height: 10,
      destroy: () => {
        destroyed.push('c');
      },
    });

    expect(pool.stats()).toEqual({
      keys: ['a', 'c'],
      textureCount: 2,
      textureMemoryBytes: 800,
    });
    expect(destroyed).toEqual(['b']);
  });

  it('HLS 预览只在 hover 或选中时触发', () => {
    expect(shouldRequestHlsPreview({ hovered: false, selected: false })).toBe(false);
    expect(shouldRequestHlsPreview({ hovered: true, selected: false })).toBe(true);
    expect(shouldRequestHlsPreview({ hovered: false, selected: true })).toBe(true);
  });

  it('指标快照只统计窗口内对象与纹理池状态', () => {
    const snapshot = createRenderMetricsSnapshot({
      hlsRequests: 1,
      thumbnailRequests: 72,
      textureStats: { keys: ['a', 'c'], textureCount: 2, textureMemoryBytes: 800 },
      window: { start: 144, end: 216 },
    });

    expect(snapshot).toEqual({
      hlsRequests: 1,
      pixiObjectCount: 72,
      textureCount: 2,
      textureMemoryBytes: 800,
      thumbnailRequests: 72,
      visibleItems: 72,
    });
  });

  it('Pixi 预览单元格保持固定数量与坐标', () => {
    expect(createPixiPreviewCells({ cellHeight: 20, cellWidth: 26, columns: 3, gap: 4, rows: 2 })).toEqual([
      { color: 0x38bdf8, height: 20, width: 26, x: 0, y: 0 },
      { color: 0x22c55e, height: 20, width: 26, x: 30, y: 0 },
      { color: 0x22c55e, height: 20, width: 26, x: 60, y: 0 },
      { color: 0x22c55e, height: 20, width: 26, x: 0, y: 24 },
      { color: 0x22c55e, height: 20, width: 26, x: 30, y: 24 },
      { color: 0x38bdf8, height: 20, width: 26, x: 60, y: 24 },
    ]);
  });

  it('挂载 Pixi 预览并暴露可销毁句柄', async () => {
    const children: unknown[] = [];
    const host = {
      replaceChildren: (...values: unknown[]) => {
        children.splice(0, children.length, ...values);
      },
    } as HTMLElement;

    const handle = await mountPixiGridPreview({ columns: 2, height: 120, host, rows: 1, width: 320 });

    expect(children).toEqual([handle.canvas]);
    expect(handle.canvas.width).toBe(320);
    expect(handle.canvas.height).toBe(120);
    expect(handle.rendererType).toBe('webgl-test');
    expect(handle.pixiVersion).toBe('mock-pixi');
    expect(() => {
      handle.destroy();
    }).not.toThrow();
  });
});
