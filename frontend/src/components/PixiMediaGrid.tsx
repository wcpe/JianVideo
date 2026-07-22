import { useEffect, useRef, useCallback, useState } from 'react';
import { Box, Text, Group, Skeleton, Alert } from '@mantine/core';
import {
  mountMediaGridSession,
  type MediaGridItem,
  type MediaGridSessionHandle,
  type MediaGridLayout,
} from '@jianvideo/render-pixi';
import type { MediaFile } from '@/types';
import { mediaDisplayName } from '@/utils/media';

export type PixiMediaGridProps = {
  items: readonly MediaFile[];
  total: number;
  loading?: boolean;
  error?: string | null;
  selectedIds: ReadonlySet<number>;
  onSelect: (mediaId: number, additive: boolean) => void;
  onOpen: (mediaId: number) => void;
  onNeedMore?: () => void;
  columns?: number;
};

function toGridItems(files: readonly MediaFile[]): MediaGridItem[] {
  return files.map((f) => ({
    id: f.id,
    title: mediaDisplayName(f),
    thumbnailUrl: `/api/library/thumbnail/${f.id}`,
    durationSeconds: f.duration || undefined,
    isVideo: true,
  }));
}

async function loadThumbnail(url: string, signal: AbortSignal): Promise<CanvasImageSource | null> {
  try {
    const res = await fetch(url, { signal, credentials: 'same-origin' });
    if (!res.ok) return null;
    const blob = await res.blob();
    if (typeof createImageBitmap === 'function') {
      return await createImageBitmap(blob);
    }
    return await new Promise<HTMLImageElement | null>((resolve, reject) => {
      const img = new Image();
      img.onload = () => resolve(img);
      img.onerror = () => reject(new Error('缩略图解码失败'));
      img.src = URL.createObjectURL(blob);
      signal.addEventListener('abort', () => {
        URL.revokeObjectURL(img.src);
        reject(new DOMException('Aborted', 'AbortError'));
      });
    });
  } catch {
    return null;
  }
}

/**
 * Pixi 高密度媒体网格热区（FR2-009）。
 * React 只持 host、筛选结果与选中态；滚动与纹理更新在 render-pixi 内完成。
 */
export default function PixiMediaGrid({
  items,
  total,
  loading,
  error,
  selectedIds,
  onSelect,
  onOpen,
  onNeedMore,
  columns = 6,
}: PixiMediaGridProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const sessionRef = useRef<MediaGridSessionHandle | null>(null);
  const [ready, setReady] = useState(false);
  const [metricsText, setMetricsText] = useState('');
  const [initError, setInitError] = useState<string | null>(null);

  const onSelectRef = useRef(onSelect);
  const onOpenRef = useRef(onOpen);
  const onNeedMoreRef = useRef(onNeedMore);
  onSelectRef.current = onSelect;
  onOpenRef.current = onOpen;
  onNeedMoreRef.current = onNeedMore;

  const layout: Partial<MediaGridLayout> = {
    columns,
    cellWidth: 160,
    cellHeight: 120,
    gap: 8,
    overscanRows: 2,
  };

  // 挂载 Pixi 会话
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    let cancelled = false;
    const rect = host.getBoundingClientRect();
    const width = Math.max(320, Math.floor(rect.width) || host.clientWidth || 800);
    const height = Math.max(240, Math.floor(rect.height) || 480);

    void mountMediaGridSession({
      host,
      width,
      height,
      layout,
      loadThumbnail,
      onSelect: (id: number, additive: boolean) => onSelectRef.current(id, additive),
      onOpen: (id: number) => onOpenRef.current(id),
      onNeedMore: () => onNeedMoreRef.current?.(),
    })
      .then((session: MediaGridSessionHandle) => {
        if (cancelled) {
          session.destroy();
          return;
        }
        sessionRef.current = session;
        setReady(true);
        setInitError(null);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setInitError(err instanceof Error ? err.message : 'Pixi 网格初始化失败');
        }
      });

    return () => {
      cancelled = true;
      sessionRef.current?.destroy();
      sessionRef.current = null;
      setReady(false);
    };
    // 仅挂载一次；columns 变化走 setLayout
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 同步 items / total
  useEffect(() => {
    sessionRef.current?.setItems(toGridItems(items), total);
  }, [items, total, ready]);

  // 同步选中
  useEffect(() => {
    sessionRef.current?.setSelection({ selectedIds });
  }, [selectedIds, ready]);

  // 列数变化
  useEffect(() => {
    sessionRef.current?.setLayout({ columns });
  }, [columns, ready]);

  // 尺寸变化
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const ro = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry || !sessionRef.current) return;
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) {
        sessionRef.current.resize(Math.floor(width), Math.floor(height));
      }
    });
    ro.observe(host);
    return () => ro.disconnect();
  }, [ready]);

  // 状态栏指标（低频）
  useEffect(() => {
    if (!ready) return;
    const timer = window.setInterval(() => {
      const m = sessionRef.current?.getMetrics();
      if (!m) return;
      setMetricsText(
        `可见 ${m.visibleItems} · 对象 ${m.pixiObjectCount} · 纹理 ${m.textureCount} · 缩略图请求 ${m.thumbnailRequests}`,
      );
    }, 1000);
    return () => window.clearInterval(timer);
  }, [ready]);

  const handleHostKey = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' && selectedIds.size === 1) {
        const id = [...selectedIds][0];
        if (id !== undefined) onOpen(id);
      }
    },
    [selectedIds, onOpen],
  );

  if (error) {
    return (
      <Alert color="red" title="加载失败">
        {error}
      </Alert>
    );
  }

  return (
    <Box style={{ display: 'flex', flexDirection: 'column', gap: 8, minHeight: 0, flex: 1 }}>
      {initError && (
        <Alert color="orange" title="Pixi 回退提示">
          {initError}（可刷新重试；无 WebGL 时网格不可用）
        </Alert>
      )}
      {loading && items.length === 0 ? (
        <Skeleton height={360} radius="md" />
      ) : (
        <Box
          ref={hostRef}
          data-testid="pixi-media-grid"
          tabIndex={0}
          onKeyDown={handleHostKey}
          style={{
            flex: 1,
            minHeight: 360,
            height: 'min(70vh, 720px)',
            borderRadius: 8,
            overflow: 'hidden',
            background: 'var(--mantine-color-dark-7)',
            outline: 'none',
          }}
          aria-label="高密度媒体网格"
        />
      )}
      <Group justify="space-between">
        <Text size="xs" c="dimmed">
          共 {total} 项 · 已加载 {items.length}
          {selectedIds.size > 0 ? ` · 已选 ${selectedIds.size}` : ''}
        </Text>
        <Text size="xs" c="dimmed" data-testid="pixi-grid-metrics">
          {metricsText || (ready ? '就绪' : '初始化中…')}
        </Text>
      </Group>
    </Box>
  );
}
