export {
  resolveVisibleWindow,
  resolveGridWindow,
  createRenderMetricsSnapshot,
} from "./window-metrics";
export type {
  VisibleWindow,
  GridWindowInput,
  GridWindow,
  RenderMetricsInput,
  RenderMetricsSnapshot,
} from "./window-metrics";

export {
  TexturePool,
  estimateTextureMemoryBytes,
  shouldRequestHlsPreview,
  createDefaultMediaTexturePool,
} from "./texture-pool";
export type {
  TextureEntry,
  TexturePoolOptions,
  TexturePoolStats,
  PreviewState,
} from "./texture-pool";

export interface PixiPreviewCellInput {
  readonly cellHeight: number;
  readonly cellWidth: number;
  readonly columns: number;
  readonly gap: number;
  readonly rows: number;
}

export interface PixiPreviewCell {
  readonly color: number;
  readonly height: number;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

export interface PixiGridPreviewOptions {
  readonly columns?: number;
  readonly height: number;
  readonly host: HTMLElement;
  readonly rows?: number;
  readonly width: number;
}

export interface PixiGridPreviewHandle {
  readonly canvas: HTMLCanvasElement;
  readonly pixiVersion: string;
  readonly rendererType: string;
  readonly destroy: () => void;
}

export function createPixiPreviewCells(
  input: PixiPreviewCellInput,
): readonly PixiPreviewCell[] {
  const cells: PixiPreviewCell[] = [];
  for (let row = 0; row < input.rows; row += 1) {
    for (let column = 0; column < input.columns; column += 1) {
      cells.push({
        color: (row + column) % 3 === 0 ? 0x38bdf8 : 0x22c55e,
        height: input.cellHeight,
        width: input.cellWidth,
        x: column * (input.cellWidth + input.gap),
        y: row * (input.cellHeight + input.gap),
      });
    }
  }
  return cells;
}

export async function mountPixiGridPreview(
  options: PixiGridPreviewOptions,
): Promise<PixiGridPreviewHandle> {
  const { Application, Graphics, VERSION } = await import("pixi.js");
  const app = new Application();
  await app.init({
    antialias: false,
    autoDensity: true,
    backgroundColor: 0x0f172a,
    height: options.height,
    powerPreference: "high-performance",
    preference: "webgl",
    preserveDrawingBuffer: true,
    resolution: globalThis.devicePixelRatio || 1,
    width: options.width,
  });

  const { canvas } = app;
  canvas.width = options.width;
  canvas.height = options.height;
  options.host.replaceChildren(canvas);

  const graphics = new Graphics();
  for (const cell of createPixiPreviewCells({
    cellHeight: 20,
    cellWidth: 26,
    columns: options.columns ?? 8,
    gap: 12,
    rows: options.rows ?? 3,
  })) {
    graphics
      .rect(12 + cell.x, 12 + cell.y, cell.width, cell.height)
      .fill(cell.color);
  }
  app.stage.addChild(graphics);
  app.render();

  return {
    canvas,
    destroy: () => {
      app.destroy({ removeView: true }, true);
    },
    pixiVersion: VERSION,
    rendererType: app.renderer.name,
  };
}

// 生产级媒体网格（FR2-009）
export {
  DEFAULT_MEDIA_GRID_LAYOUT,
  buildMediaGridFrame,
  collectThumbnailRequests,
  hitTestMediaGrid,
  mediaTextureKey,
  resolveGridContentHeight,
  resolveMediaGridWindow,
  snapshotMediaGridMetrics,
} from "./media-grid";
export type {
  MediaGridCellRect,
  MediaGridFrame,
  MediaGridItem,
  MediaGridLayout,
  MediaGridSelection,
  MediaGridViewport,
} from "./media-grid";
export { mountMediaGridSession } from "./media-grid-session";
export type {
  MediaGridSessionHandle,
  MediaGridSessionOptions,
} from "./media-grid-session";
