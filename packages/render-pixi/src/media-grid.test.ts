import { describe, expect, it } from "vitest";
import {
  buildMediaGridFrame,
  collectThumbnailRequests,
  hitTestMediaGrid,
  mediaTextureKey,
  resolveGridContentHeight,
  resolveMediaGridWindow,
  snapshotMediaGridMetrics,
  DEFAULT_MEDIA_GRID_LAYOUT,
} from "./media-grid";
import type { MediaGridItem } from "./media-grid";

function items(n: number): MediaGridItem[] {
  return Array.from({ length: n }, (_, i) => ({
    id: i + 1,
    title: `m${String(i + 1)}`,
    thumbnailUrl: `/t/${String(i + 1)}`,
    isVideo: true,
  }));
}

describe("media-grid (FR2-009)", () => {
  it("纹理 key 含 media_id 与 tier", () => {
    expect(mediaTextureKey(42)).toBe("42:thumb");
    expect(mediaTextureKey(42, "hq")).toBe("42:hq");
  });

  it("内容高度随行数增长、不随列数线性爆炸", () => {
    const layout = { columns: 4, cellHeight: 100, gap: 10 };
    // 8 项 → 2 行：2*110 - 10 = 210
    expect(resolveGridContentHeight(8, layout)).toBe(210);
    expect(resolveGridContentHeight(0, layout)).toBe(0);
  });

  it("可见窗口只覆盖视口 + overscan", () => {
    const win = resolveMediaGridWindow(
      1_000_000,
      { width: 800, height: 600, scrollTop: 0 },
      { ...DEFAULT_MEDIA_GRID_LAYOUT, columns: 6, cellHeight: 100, gap: 0, overscanRows: 1 },
    );
    // 可视 6 行 * 6 列 = 36，overscan 上下各 1 行 → 最多 8*6=48
    expect(win.start).toBe(0);
    expect(win.end).toBeLessThanOrEqual(48);
    expect(win.end - win.start).toBeLessThan(100);
  });

  it("buildMediaGridFrame 只生成窗口内 cell", () => {
    const list = items(100);
    const frame = buildMediaGridFrame({
      total: 100,
      items: list,
      viewport: { width: 700, height: 240, scrollTop: 0 },
      layout: { columns: 4, cellWidth: 160, cellHeight: 100, gap: 10, overscanRows: 0 },
      selection: { selectedIds: new Set([2]), hoveredId: 3 },
    });
    expect(frame.cells.length).toBeGreaterThan(0);
    expect(frame.cells.length).toBeLessThanOrEqual(12);
    expect(frame.cells.every((c) => c.index < list.length)).toBe(true);
    const selected = frame.cells.find((c) => c.id === 2);
    expect(selected?.selected).toBe(true);
    const hovered = frame.cells.find((c) => c.id === 3);
    expect(hovered?.hovered).toBe(true);
  });

  it("hitTest 命中与未命中", () => {
    const list = items(4);
    const frame = buildMediaGridFrame({
      total: 4,
      items: list,
      viewport: { width: 400, height: 200, scrollTop: 0 },
      layout: { columns: 2, cellWidth: 100, cellHeight: 80, gap: 10, overscanRows: 0 },
      selection: { selectedIds: new Set(), hoveredId: null },
    });
    // 第一格 (0,0)-(100,80)
    expect(hitTestMediaGrid(frame, 10, 10)).toBe(1);
    expect(hitTestMediaGrid(frame, 999, 999)).toBeNull();
  });

  it("缩略图请求跳过已在池中的 key", () => {
    const list = items(10);
    const reqs = collectThumbnailRequests(
      list,
      { start: 0, end: 4 },
      new Set([mediaTextureKey(1), mediaTextureKey(2)]),
    );
    expect(reqs.map((r) => r.id)).toEqual([3, 4]);
  });

  it("指标快照 visibleItems 取窗口长度", () => {
    const snap = snapshotMediaGridMetrics({
      window: { start: 10, end: 30 },
      textureStats: { keys: ["a"], textureCount: 1, textureMemoryBytes: 100 },
      thumbnailRequests: 5,
      hlsRequests: 1,
    });
    expect(snap.visibleItems).toBe(20);
    expect(snap.pixiObjectCount).toBe(20);
    expect(snap.thumbnailRequests).toBe(5);
  });
});
