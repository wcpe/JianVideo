import {
  buildMediaGridFrame,
  collectThumbnailRequests,
  hitTestMediaGrid,
  mediaTextureKey,
  snapshotMediaGridMetrics,
  type MediaGridFrame,
  type MediaGridItem,
  type MediaGridLayout,
  type MediaGridSelection,
  type MediaGridViewport,
  DEFAULT_MEDIA_GRID_LAYOUT,
} from "./media-grid";
import type { RenderMetricsSnapshot } from "./window-metrics";
import {
  createDefaultMediaTexturePool,
  shouldRequestHlsPreview,
  type TexturePool,
} from "./texture-pool";

export interface MediaGridSessionOptions {
  readonly host: HTMLElement;
  readonly width: number;
  readonly height: number;
  readonly layout?: Partial<MediaGridLayout>;
  readonly backgroundColor?: number;
  /** 缩略图加载器：返回可绘制 ImageBitmap / HTMLImageElement；失败返回 null。 */
  readonly loadThumbnail?: (
    url: string,
    signal: AbortSignal,
  ) => Promise<CanvasImageSource | null>;
  readonly onSelect?: (mediaId: number, additive: boolean) => void;
  readonly onOpen?: (mediaId: number) => void;
  readonly onHoverChange?: (mediaId: number | null) => void;
  readonly onNeedMore?: () => void;
  readonly onPreviewRequest?: (mediaId: number) => void;
}

export interface MediaGridSessionHandle {
  readonly canvas: HTMLCanvasElement;
  readonly pixiVersion: string;
  readonly rendererType: string;
  setItems(items: readonly MediaGridItem[], total: number): void;
  setViewport(viewport: Partial<MediaGridViewport>): void;
  setSelection(selection: Partial<MediaGridSelection>): void;
  setLayout(layout: Partial<MediaGridLayout>): void;
  resize(width: number, height: number): void;
  getMetrics(): RenderMetricsSnapshot;
  getFrame(): MediaGridFrame;
  destroy(): void;
}

/**
 * 挂载生产级媒体网格热区（FR2-009）。
 * React 只持 host 与控制态；滚动/纹理更新不经 React 重渲染。
 */
export async function mountMediaGridSession(
  options: MediaGridSessionOptions,
): Promise<MediaGridSessionHandle> {
  const {
    Application,
    Container,
    Graphics,
    Sprite,
    Texture,
    VERSION,
  } = await import("pixi.js");

  let layout: MediaGridLayout = {
    ...DEFAULT_MEDIA_GRID_LAYOUT,
    ...options.layout,
  };
  let viewport: MediaGridViewport = {
    width: options.width,
    height: options.height,
    scrollTop: 0,
  };
  let items: readonly MediaGridItem[] = [];
  let total = 0;
  let selection: MediaGridSelection = {
    selectedIds: new Set<number>(),
    hoveredId: null,
  };
  let thumbnailRequests = 0;
  let hlsRequests = 0;
  let destroyed = false;
  let loadGeneration = 0;
  const abortByKey = new Map<string, AbortController>();
  const texturePool: TexturePool = createDefaultMediaTexturePool();
  // 将真实 Texture 与池条目绑定（池只管 key 与销毁）
  const liveTextures = new Map<string, InstanceType<typeof Texture>>();

  const app = new Application();
  await app.init({
    antialias: false,
    autoDensity: true,
    backgroundColor: options.backgroundColor ?? 0x0f172a,
    height: options.height,
    powerPreference: "high-performance",
    preference: "webgl",
    resolution: globalThis.devicePixelRatio || 1,
    width: options.width,
  });

  const { canvas } = app;
  options.host.replaceChildren(canvas);
  canvas.style.display = "block";
  canvas.style.width = "100%";
  canvas.style.height = "100%";
  canvas.style.touchAction = "none";

  const root = new Container();
  app.stage.addChild(root);

  // 简单对象池：复用 Graphics + Sprite 节点
  type CellNode = {
    container: InstanceType<typeof Container>;
    bg: InstanceType<typeof Graphics>;
    border: InstanceType<typeof Graphics>;
    sprite: InstanceType<typeof Sprite>;
  };
  const nodePool: CellNode[] = [];
  const activeNodes = new Map<number, CellNode>();

  function acquireNode(): CellNode {
    const recycled = nodePool.pop();
    if (recycled) return recycled;
    const container = new Container();
    container.eventMode = "static";
    container.cursor = "pointer";
    const bg = new Graphics();
    const border = new Graphics();
    const sprite = new Sprite();
    container.addChild(bg);
    container.addChild(sprite);
    container.addChild(border);
    return { container, bg, border, sprite };
  }

  function releaseNode(node: CellNode): void {
    node.sprite.texture = Texture.EMPTY;
    // pixi v8 事件 mixin 在部分解析路径下不在 Container 静态类型上暴露
    const target = node.container as unknown as {
      removeAllListeners: () => void;
    };
    target.removeAllListeners();
    root.removeChild(node.container);
    nodePool.push(node);
  }

  function currentFrame(): MediaGridFrame {
    return buildMediaGridFrame({
      total,
      items,
      viewport,
      layout,
      selection,
    });
  }

  function paintFrame(frame: MediaGridFrame): void {
    if (destroyed) return;
    const keep = new Set(frame.cells.map((c) => c.id));
    for (const [id, node] of activeNodes) {
      if (!keep.has(id)) {
        activeNodes.delete(id);
        releaseNode(node);
      }
    }
    for (const cell of frame.cells) {
      let node = activeNodes.get(cell.id);
      if (!node) {
        node = acquireNode();
        activeNodes.set(cell.id, node);
        root.addChild(node.container);
      }
      node.container.x = cell.x;
      node.container.y = cell.y;

      node.bg.clear();
      node.bg.roundRect(0, 0, cell.width, cell.height, 6).fill(0x1e293b);

      const key = mediaTextureKey(cell.id);
      const tex = liveTextures.get(key);
      if (tex) {
        node.sprite.texture = tex;
        node.sprite.visible = true;
        // cover 裁剪
        const tw = tex.width || 1;
        const th = tex.height || 1;
        const scale = Math.max(cell.width / tw, cell.height / th);
        node.sprite.width = tw * scale;
        node.sprite.height = th * scale;
        node.sprite.x = (cell.width - node.sprite.width) / 2;
        node.sprite.y = (cell.height - node.sprite.height) / 2;
      } else {
        node.sprite.texture = Texture.EMPTY;
        node.sprite.visible = false;
      }

      node.border.clear();
      if (cell.selected || cell.hovered) {
        const color = cell.selected ? 0xa78bfa : 0x38bdf8;
        node.border
          .roundRect(1, 1, cell.width - 2, cell.height - 2, 6)
          .stroke({ width: 2, color });
      }

      const mediaId = cell.id;
      // 事件 API 通过运行期 mixin 挂载；用窄断言绑定 pointer 事件
      const interactive = node.container as unknown as {
        removeAllListeners: () => void;
        on: (event: string, fn: (e?: { shiftKey?: boolean; ctrlKey?: boolean; metaKey?: boolean }) => void) => void;
      };
      interactive.removeAllListeners();
      interactive.on("pointerover", () => {
        selection = { ...selection, hoveredId: mediaId };
        options.onHoverChange?.(mediaId);
        if (
          shouldRequestHlsPreview({
            hovered: true,
            selected: selection.selectedIds.has(mediaId),
          })
        ) {
          hlsRequests += 1;
          options.onPreviewRequest?.(mediaId);
        }
        scheduleRender();
      });
      interactive.on("pointerout", () => {
        if (selection.hoveredId === mediaId) {
          selection = { ...selection, hoveredId: null };
          options.onHoverChange?.(null);
          scheduleRender();
        }
      });
      interactive.on("pointertap", (event) => {
        const additive = !!(event?.shiftKey || event?.ctrlKey || event?.metaKey);
        options.onSelect?.(mediaId, additive);
      });
    }
    app.render();
  }

  function requestVisibleThumbnails(frame: MediaGridFrame): void {
    if (!options.loadThumbnail) return;
    const poolKeys = new Set(texturePool.stats().keys);
    const pending = collectThumbnailRequests(items, frame.window, poolKeys);
    for (const req of pending) {
      if (abortByKey.has(req.key)) continue;
      const controller = new AbortController();
      abortByKey.set(req.key, controller);
      thumbnailRequests += 1;
      const gen = loadGeneration;
      void options
        .loadThumbnail(req.url, controller.signal)
        .then((source) => {
          if (destroyed || gen !== loadGeneration || !source) return;
          const texture = Texture.from(source);
          liveTextures.set(req.key, texture);
          texturePool.put({
            key: req.key,
            width: texture.width || layout.cellWidth,
            height: texture.height || layout.cellHeight,
            destroy: () => {
              liveTextures.delete(req.key);
              texture.destroy(true);
            },
          });
          scheduleRender();
        })
        .catch(() => {
          /* 快速滚动取消或加载失败：静默 */
        })
        .finally(() => {
          abortByKey.delete(req.key);
        });
    }
    // 窗口外进行中的请求取消
    const needed = new Set(pending.map((p) => p.key));
    for (const [key, ctrl] of abortByKey) {
      if (!needed.has(key) && !poolKeys.has(key)) {
        ctrl.abort();
        abortByKey.delete(key);
      }
    }
  }

  let raf = 0;
  function scheduleRender(): void {
    if (destroyed || raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      const frame = currentFrame();
      paintFrame(frame);
      requestVisibleThumbnails(frame);
      // 接近底部时通知壳层加载更多
      const nearBottom =
        viewport.scrollTop + viewport.height >= frame.contentHeight - viewport.height;
      if (nearBottom && items.length < total) {
        options.onNeedMore?.();
      }
    });
  }

  // 滚轮：更新 scrollTop，不经 React
  const onWheel = (event: WheelEvent) => {
    event.preventDefault();
    const frame = currentFrame();
    const maxScroll = Math.max(0, frame.contentHeight - viewport.height);
    const next = Math.min(maxScroll, Math.max(0, viewport.scrollTop + event.deltaY));
    if (next === viewport.scrollTop) return;
    viewport = { ...viewport, scrollTop: next };
    scheduleRender();
  };
  canvas.addEventListener("wheel", onWheel, { passive: false });

  // 双击打开
  const onDblClick = (event: MouseEvent) => {
    const rect = canvas.getBoundingClientRect();
    const x = event.clientX - rect.left;
    const y = event.clientY - rect.top;
    const id = hitTestMediaGrid(currentFrame(), x, y);
    if (id !== null) options.onOpen?.(id);
  };
  canvas.addEventListener("dblclick", onDblClick);

  scheduleRender();

  return {
    canvas,
    pixiVersion: VERSION,
    rendererType: app.renderer.name,
    setItems(nextItems, nextTotal) {
      items = nextItems;
      total = Math.max(0, nextTotal);
      loadGeneration += 1;
      scheduleRender();
    },
    setViewport(partial) {
      viewport = { ...viewport, ...partial };
      scheduleRender();
    },
    setSelection(partial) {
      selection = {
        selectedIds: partial.selectedIds ?? selection.selectedIds,
        hoveredId:
          partial.hoveredId !== undefined ? partial.hoveredId : selection.hoveredId,
      };
      scheduleRender();
    },
    setLayout(partial) {
      layout = { ...layout, ...partial };
      scheduleRender();
    },
    resize(width, height) {
      viewport = { ...viewport, width, height };
      app.renderer.resize(width, height);
      scheduleRender();
    },
    getMetrics() {
      const frame = currentFrame();
      return snapshotMediaGridMetrics({
        window: frame.window,
        textureStats: texturePool.stats(),
        thumbnailRequests,
        hlsRequests,
      });
    },
    getFrame() {
      return currentFrame();
    },
    destroy() {
      if (destroyed) return;
      destroyed = true;
      if (raf) cancelAnimationFrame(raf);
      canvas.removeEventListener("wheel", onWheel);
      canvas.removeEventListener("dblclick", onDblClick);
      for (const ctrl of abortByKey.values()) ctrl.abort();
      abortByKey.clear();
      for (const node of activeNodes.values()) releaseNode(node);
      activeNodes.clear();
      // 销毁所有已加载纹理（纹理池 put 时的 destroy 回调会清理 liveTextures）
      for (const [key, texture] of liveTextures) {
        texture.destroy(true);
        liveTextures.delete(key);
      }
      app.destroy({ removeView: true }, true);
      options.host.replaceChildren();
    },
  };
}
