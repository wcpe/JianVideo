/** 可见窗口与渲染指标（从 index 抽离，避免 media-grid 循环依赖）。 */

export interface VisibleWindow {
  readonly start: number;
  readonly end: number;
}

export interface GridWindowInput {
  readonly total: number;
  readonly columns: number;
  readonly itemHeight: number;
  readonly scrollTop: number;
  readonly viewportHeight: number;
  readonly overscanRows: number;
}

export interface GridWindow extends VisibleWindow {
  readonly firstVisible: number;
  readonly visibleCount: number;
}

export interface TexturePoolStats {
  readonly keys: readonly string[];
  readonly textureCount: number;
  readonly textureMemoryBytes: number;
}

export interface RenderMetricsInput {
  readonly hlsRequests: number;
  readonly thumbnailRequests: number;
  readonly textureStats: TexturePoolStats;
  readonly window: VisibleWindow;
}

export interface RenderMetricsSnapshot {
  readonly hlsRequests: number;
  readonly pixiObjectCount: number;
  readonly textureCount: number;
  readonly textureMemoryBytes: number;
  readonly thumbnailRequests: number;
  readonly visibleItems: number;
}

export function resolveVisibleWindow(
  total: number,
  firstVisible: number,
  visibleCount: number,
  overscan: number,
): VisibleWindow {
  const start = Math.max(0, firstVisible - overscan);
  const end = Math.min(total, firstVisible + visibleCount + overscan);
  return { start, end };
}

export function resolveGridWindow(input: GridWindowInput): GridWindow {
  const total = Math.max(0, input.total);
  const columns = Math.max(1, input.columns);
  const itemHeight = Math.max(1, input.itemHeight);
  const firstRow = Math.max(0, Math.floor(input.scrollTop / itemHeight));
  const visibleRows = Math.max(1, Math.ceil(input.viewportHeight / itemHeight));
  const overscanRows = Math.max(0, input.overscanRows);
  const firstVisible = Math.min(total, firstRow * columns);
  const visibleCount = Math.min(total - firstVisible, visibleRows * columns);
  const start = Math.max(0, (firstRow - overscanRows) * columns);
  const end = Math.min(
    total,
    (firstRow + visibleRows + overscanRows) * columns,
  );
  return { start, end, firstVisible, visibleCount };
}

export function createRenderMetricsSnapshot(
  input: RenderMetricsInput,
): RenderMetricsSnapshot {
  const visibleItems = Math.max(0, input.window.end - input.window.start);
  return {
    hlsRequests: input.hlsRequests,
    pixiObjectCount: visibleItems,
    textureCount: input.textureStats.textureCount,
    textureMemoryBytes: input.textureStats.textureMemoryBytes,
    thumbnailRequests: input.thumbnailRequests,
    visibleItems,
  };
}
