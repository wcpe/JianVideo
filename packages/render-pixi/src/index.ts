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

export interface TextureEntry {
  readonly key: string;
  readonly width: number;
  readonly height: number;
  readonly destroy: () => void;
}

export interface TexturePoolOptions {
  readonly maxTextures: number;
  readonly maxBytes: number;
}

export interface TexturePoolStats {
  readonly keys: readonly string[];
  readonly textureCount: number;
  readonly textureMemoryBytes: number;
}

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

export interface PreviewState {
  readonly hovered: boolean;
  readonly selected: boolean;
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

export function resolveVisibleWindow(total: number, firstVisible: number, visibleCount: number, overscan: number): VisibleWindow {
  const start = Math.max(0, firstVisible - overscan);
  const end = Math.min(total, firstVisible + visibleCount + overscan);
  return { start, end };
}

export function estimateTextureMemoryBytes(textureCount: number, width: number, height: number): number {
  return textureCount * width * height * 4;
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
  const end = Math.min(total, (firstRow + visibleRows + overscanRows) * columns);
  return { start, end, firstVisible, visibleCount };
}

export function createPixiPreviewCells(input: PixiPreviewCellInput): readonly PixiPreviewCell[] {
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

export async function mountPixiGridPreview(options: PixiGridPreviewOptions): Promise<PixiGridPreviewHandle> {
  const { Application, Graphics, VERSION } = await import('pixi.js');
  const app = new Application();
  await app.init({
    antialias: false,
    autoDensity: true,
    backgroundColor: 0x0f172a,
    height: options.height,
    powerPreference: 'high-performance',
    preference: 'webgl',
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
    graphics.rect(12 + cell.x, 12 + cell.y, cell.width, cell.height).fill(cell.color);
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

export class TexturePool {
  readonly #entries = new Map<string, TextureEntry>();
  readonly #options: TexturePoolOptions;
  #textureMemoryBytes = 0;

  constructor(options: TexturePoolOptions) {
    this.#options = options;
  }

  get(key: string): TextureEntry | undefined {
    const entry = this.#entries.get(key);
    if (entry === undefined) {
      return undefined;
    }
    this.#entries.delete(key);
    this.#entries.set(key, entry);
    return entry;
  }

  put(entry: TextureEntry): void {
    const existing = this.#entries.get(entry.key);
    if (existing !== undefined) {
      this.#textureMemoryBytes -= textureEntryBytes(existing);
      existing.destroy();
      this.#entries.delete(entry.key);
    }
    this.#entries.set(entry.key, entry);
    this.#textureMemoryBytes += textureEntryBytes(entry);
    this.#evictUntilWithinBudget();
  }

  stats(): TexturePoolStats {
    return {
      keys: [...this.#entries.keys()],
      textureCount: this.#entries.size,
      textureMemoryBytes: this.#textureMemoryBytes,
    };
  }

  #evictUntilWithinBudget(): void {
    while (this.#entries.size > this.#options.maxTextures || this.#textureMemoryBytes > this.#options.maxBytes) {
      const oldest = this.#entries.entries().next().value;
      if (oldest === undefined) {
        return;
      }
      const [key, entry] = oldest;
      this.#entries.delete(key);
      this.#textureMemoryBytes -= textureEntryBytes(entry);
      entry.destroy();
    }
  }
}

export function shouldRequestHlsPreview(state: PreviewState): boolean {
  return state.hovered || state.selected;
}

export function createRenderMetricsSnapshot(input: RenderMetricsInput): RenderMetricsSnapshot {
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

function textureEntryBytes(entry: TextureEntry): number {
  return estimateTextureMemoryBytes(1, entry.width, entry.height);
}
