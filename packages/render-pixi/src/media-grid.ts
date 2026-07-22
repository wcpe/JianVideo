// 注意：不从 ./index 再导出本模块，避免循环依赖；仅使用同包已落地的纯函数类型。
import {
  createRenderMetricsSnapshot,
  resolveGridWindow,
  type GridWindow,
  type RenderMetricsSnapshot,
  type TexturePoolStats,
  type VisibleWindow,
} from "./window-metrics";

/** 生产网格单元（FR2-009）：前端传入的轻量媒体描述。 */
export interface MediaGridItem {
  readonly id: number;
  readonly title: string;
  readonly thumbnailUrl: string;
  readonly durationSeconds?: number;
  readonly isVideo?: boolean;
}

export interface MediaGridLayout {
  readonly columns: number;
  readonly cellWidth: number;
  readonly cellHeight: number;
  readonly gap: number;
  readonly overscanRows: number;
}

export interface MediaGridViewport {
  readonly width: number;
  readonly height: number;
  readonly scrollTop: number;
}

export interface MediaGridSelection {
  readonly selectedIds: ReadonlySet<number>;
  readonly hoveredId: number | null;
}

export interface MediaGridCellRect {
  readonly index: number;
  readonly id: number;
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
  readonly selected: boolean;
  readonly hovered: boolean;
}

export interface MediaGridFrame {
  readonly total: number;
  readonly contentHeight: number;
  readonly window: GridWindow;
  readonly cells: readonly MediaGridCellRect[];
}

export const DEFAULT_MEDIA_GRID_LAYOUT: MediaGridLayout = {
  columns: 6,
  cellWidth: 160,
  cellHeight: 120,
  gap: 8,
  overscanRows: 2,
};

/** 纹理 key：media_id + tier，便于缓存分层。 */
export function mediaTextureKey(mediaId: number, tier = "thumb"): string {
  return `${String(mediaId)}:${tier}`;
}

/** 计算网格内容总高度（像素）。 */
export function resolveGridContentHeight(
  total: number,
  layout: Pick<MediaGridLayout, "columns" | "cellHeight" | "gap">,
): number {
  if (total <= 0) return 0;
  const columns = Math.max(1, layout.columns);
  const rows = Math.ceil(total / columns);
  const stride = layout.cellHeight + layout.gap;
  return Math.max(0, rows * stride - layout.gap);
}

/** 由布局与视口解析可见窗口。 */
export function resolveMediaGridWindow(
  total: number,
  viewport: MediaGridViewport,
  layout: MediaGridLayout,
): GridWindow {
  const stride = layout.cellHeight + layout.gap;
  return resolveGridWindow({
    total,
    columns: layout.columns,
    itemHeight: Math.max(1, stride),
    scrollTop: viewport.scrollTop,
    viewportHeight: viewport.height,
    overscanRows: layout.overscanRows,
  });
}

/**
 * 根据已加载 items 切片与全局 total 生成本帧单元格几何。
 * items 应对齐全局索引 [0, items.length)；窗口越界部分不生成 cell。
 */
export function buildMediaGridFrame(input: {
  readonly total: number;
  readonly items: readonly MediaGridItem[];
  readonly viewport: MediaGridViewport;
  readonly layout: MediaGridLayout;
  readonly selection: MediaGridSelection;
}): MediaGridFrame {
  const { total, items, viewport, layout, selection } = input;
  const window = resolveMediaGridWindow(total, viewport, layout);
  const contentHeight = resolveGridContentHeight(total, layout);
  const strideX = layout.cellWidth + layout.gap;
  const strideY = layout.cellHeight + layout.gap;
  const cells: MediaGridCellRect[] = [];
  const end = Math.min(window.end, items.length, total);
  for (let index = window.start; index < end; index += 1) {
    const item = items[index];
    if (!item) continue;
    const col = index % layout.columns;
    const row = Math.floor(index / layout.columns);
    cells.push({
      index,
      id: item.id,
      x: col * strideX,
      y: row * strideY - viewport.scrollTop,
      width: layout.cellWidth,
      height: layout.cellHeight,
      selected: selection.selectedIds.has(item.id),
      hovered: selection.hoveredId === item.id,
    });
  }
  return { total, contentHeight, window, cells };
}

/** 命中测试：视口坐标 → 媒体 id；未命中返回 null。 */
export function hitTestMediaGrid(
  frame: MediaGridFrame,
  localX: number,
  localY: number,
): number | null {
  for (const cell of frame.cells) {
    if (
      localX >= cell.x &&
      localX < cell.x + cell.width &&
      localY >= cell.y &&
      localY < cell.y + cell.height
    ) {
      return cell.id;
    }
  }
  return null;
}

/** 收集窗口内需要请求缩略图的 id（跳过已在纹理池中的 key）。 */
export function collectThumbnailRequests(
  items: readonly MediaGridItem[],
  window: VisibleWindow,
  poolKeys: ReadonlySet<string>,
  tier = "thumb",
): readonly { id: number; url: string; key: string }[] {
  const out: { id: number; url: string; key: string }[] = [];
  const end = Math.min(window.end, items.length);
  for (let i = window.start; i < end; i += 1) {
    const item = items[i];
    if (!item || !item.thumbnailUrl) continue;
    const key = mediaTextureKey(item.id, tier);
    if (poolKeys.has(key)) continue;
    out.push({ id: item.id, url: item.thumbnailUrl, key });
  }
  return out;
}

/** 生成指标快照（供 Benchmark / 状态栏）。 */
export function snapshotMediaGridMetrics(input: {
  readonly window: VisibleWindow;
  readonly textureStats: TexturePoolStats;
  readonly thumbnailRequests: number;
  readonly hlsRequests: number;
}): RenderMetricsSnapshot {
  return createRenderMetricsSnapshot({
    window: input.window,
    textureStats: input.textureStats,
    thumbnailRequests: input.thumbnailRequests,
    hlsRequests: input.hlsRequests,
  });
}
