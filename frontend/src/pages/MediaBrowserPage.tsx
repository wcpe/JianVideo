import { useState, useCallback, useRef, useEffect } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  Stack,
  Group,
  SegmentedControl,
  Text,
  Button,
  Box,
  NativeSelect,
  Drawer,
  ActionIcon,
  Tooltip,
} from '@mantine/core';
import { useDisclosure, useMediaQuery } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import { IconFilter, IconFilterOff, IconRefresh } from '@tabler/icons-react';
import PageHeader from '@/components/PageHeader';
import CategoryFilter from '@/components/CategoryFilter';
import LibraryFilter from '@/components/LibraryFilter';
import MediaQueryFilters from '@/components/MediaQueryFilters';
import MediaDetailPanel from '@/components/MediaDetailPanel';
import PixiMediaGrid from '@/components/PixiMediaGrid';
import BatchActionsModals from '@/components/BatchActionsModals';
import ConfirmModal from '@/components/ConfirmModal';
import { useInfiniteMedia } from '@/hooks/useInfiniteMedia';
import { useLibraryPaths } from '@/hooks/useLibraryPaths';
import { useBatchActions } from '@/hooks/useBatchActions';
import { extractErrorMessage } from '@/utils/error';
import * as libApi from '@/api/library';
import { isImageFile } from '@/utils/media';
import type { MediaFile } from '@/types';

/**
 * Pixi 高密度媒体浏览器（FR2-009）。
 * 壳层：筛选/排序/详情/批量；热区：PixiMediaGrid。
 */
export default function MediaBrowserPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const urlSearch = searchParams.get('search') ?? '';

  const [favorite, setFavorite] = useState(false);
  const [tagId, setTagId] = useState(0);
  const [libraryId, setLibraryId] = useState(0);
  const [mediaType, setMediaType] = useState<'' | 'image' | 'video'>('');
  const [sizeMin, setSizeMin] = useState(0);
  const [durationMin, setDurationMin] = useState(0);
  const [heightMin, setHeightMin] = useState(0);
  const [timeFrom, setTimeFrom] = useState('');
  const [timeTo, setTimeTo] = useState('');
  const [inferenceStatus, setInferenceStatus] = useState<
    '' | 'inferred' | 'auto' | 'manual' | 'missing'
  >('');
  const [sort, setSort] = useState('time_desc');
  const [columns, setColumns] = useState(6);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [detailIndex, setDetailIndex] = useState<number | null>(null);
  const [pendingDelete, setPendingDelete] = useState<number[]>([]);
  const [deleting, setDeleting] = useState(false);
  const [filterDrawerOpened, filterDrawer] = useDisclosure(false);
  const isNarrow = useMediaQuery('(max-width: 48em)') ?? false;

  const paths = useLibraryPaths(undefined);
  const exts = paths.customImageExtensions;

  const infinite = useInfiniteMedia({
    favorite,
    tagId,
    libraryId,
    mediaType,
    sizeMin,
    durationMin,
    heightMin,
    timeFrom,
    timeTo,
    inference: inferenceStatus,
    sort,
    initialSearch: urlSearch,
    pageSize: 120,
  });

  // 页眉全局搜索联动
  useEffect(() => {
    infinite.setSearchInput(urlSearch);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlSearch]);

  const filterActive =
    favorite ||
    tagId > 0 ||
    libraryId > 0 ||
    !!mediaType ||
    sizeMin > 0 ||
    durationMin > 0 ||
    heightMin > 0 ||
    !!timeFrom ||
    !!timeTo ||
    !!inferenceStatus;

  const clearFilters = useCallback(() => {
    setFavorite(false);
    setTagId(0);
    setLibraryId(0);
    setMediaType('');
    setSizeMin(0);
    setDurationMin(0);
    setHeightMin(0);
    setTimeFrom('');
    setTimeTo('');
    setInferenceStatus('');
  }, []);

  const handleSelect = useCallback((mediaId: number, additive: boolean) => {
    setSelectedIds((prev) => {
      if (!additive) return new Set([mediaId]);
      const next = new Set(prev);
      if (next.has(mediaId)) next.delete(mediaId);
      else next.add(mediaId);
      return next;
    });
  }, []);

  const handleOpen = useCallback(
    (mediaId: number) => {
      const idx = infinite.items.findIndex((m) => m.id === mediaId);
      const file = idx >= 0 ? infinite.items[idx] : null;
      if (file && isImageFile(file, exts)) {
        setDetailIndex(idx);
        return;
      }
      navigate(`/play/${mediaId}`);
    },
    [infinite.items, exts, navigate],
  );

  const handleToggleFavorite = useCallback(
    async (file: MediaFile) => {
      try {
        await libApi.setMediaFavorite(file.id, !file.favorite);
        infinite.reload();
      } catch (err) {
        notifications.show({ color: 'red', message: extractErrorMessage(err, '更新收藏失败') });
      }
    },
    [infinite],
  );

  const batch = useBatchActions();

  const confirmDelete = useCallback(async () => {
    if (pendingDelete.length === 0) return;
    setDeleting(true);
    try {
      const n = await libApi.batchDeleteMediaFiles(pendingDelete);
      notifications.show({ color: 'green', message: `已删除 ${n} 项到回收站` });
      setSelectedIds(new Set());
      setPendingDelete([]);
      infinite.reload();
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '批量删除失败') });
    } finally {
      setDeleting(false);
    }
  }, [pendingDelete, infinite]);

  // 避免 eslint 对 selectedList 无用告警：批量入口直接读 selectedIds
  const selectedRef = useRef(selectedIds);
  selectedRef.current = selectedIds;

  const filtersContent = (
    <Stack gap="sm">
      <CategoryFilter
        favorite={favorite}
        onFavoriteChange={setFavorite}
        tagId={tagId}
        onTagIdChange={setTagId}
      />
      <LibraryFilter libraryId={libraryId} onLibraryIdChange={setLibraryId} />
      <MediaQueryFilters
        mediaType={mediaType}
        onMediaTypeChange={setMediaType}
        sizeMin={sizeMin}
        onSizeMinChange={setSizeMin}
        durationMin={durationMin}
        onDurationMinChange={setDurationMin}
        heightMin={heightMin}
        onHeightMinChange={setHeightMin}
        timeFrom={timeFrom}
        onTimeFromChange={setTimeFrom}
        timeTo={timeTo}
        onTimeToChange={setTimeTo}
      />
      <NativeSelect
        aria-label="影视信息筛选"
        size="xs"
        value={inferenceStatus}
        onChange={(e) =>
          setInferenceStatus(
            e.currentTarget.value as '' | 'inferred' | 'auto' | 'manual' | 'missing',
          )
        }
        data={[
          { value: '', label: '全部影视信息' },
          { value: 'inferred', label: '已推断' },
          { value: 'auto', label: '自动' },
          { value: 'manual', label: '人工' },
          { value: 'missing', label: '未推断' },
        ]}
      />
    </Stack>
  );

  return (
    <Stack gap="md" style={{ minHeight: 0, flex: 1 }}>
      <PageHeader title="媒体网格" showTitle />
      <Text size="sm" c="dimmed">
        PixiJS 高密度浏览（FR2-009）：窗口化渲染、筛选走后端、滚动热区不经 React 每帧重绘
      </Text>

      <Group justify="space-between" wrap="wrap" gap="sm">
        <Group gap="sm">
          <NativeSelect
            aria-label="排序"
            value={sort}
            onChange={(e) => setSort(e.currentTarget.value)}
            data={[
              { value: 'time_desc', label: '最新优先' },
              { value: 'time_asc', label: '最早优先' },
              { value: 'name', label: '名称' },
              { value: 'size_desc', label: '体积大→小' },
              { value: 'duration', label: '时长' },
              { value: 'resolution', label: '分辨率' },
            ]}
          />
          <SegmentedControl
            size="xs"
            value={String(columns)}
            onChange={(v) => setColumns(Number(v))}
            data={[
              { value: '4', label: '4 列' },
              { value: '6', label: '6 列' },
              { value: '8', label: '8 列' },
            ]}
            aria-label="网格列数"
          />
          <Tooltip label="刷新">
            <ActionIcon variant="subtle" aria-label="刷新" onClick={() => infinite.reload()}>
              <IconRefresh size={16} />
            </ActionIcon>
          </Tooltip>
          {isNarrow && (
            <Button
              variant="light"
              leftSection={<IconFilter size={14} />}
              onClick={filterDrawer.open}
            >
              筛选
            </Button>
          )}
        </Group>
        <Group gap="sm">
          {selectedIds.size > 0 && (
            <>
              <Text size="sm">已选 {selectedIds.size}</Text>
              <Button
                size="xs"
                variant="light"
                onClick={() => void batch.openAddToAlbum([...selectedRef.current])}
              >
                加相册
              </Button>
              <Button
                size="xs"
                variant="light"
                onClick={() => void batch.openTranscode([...selectedRef.current])}
              >
                转码
              </Button>
              <Button
                size="xs"
                variant="light"
                onClick={() => void batch.openMove([...selectedRef.current])}
              >
                移动
              </Button>
              <Button
                size="xs"
                color="red"
                variant="light"
                onClick={() => setPendingDelete([...selectedRef.current])}
              >
                删除
              </Button>
              <Button size="xs" variant="subtle" onClick={() => setSelectedIds(new Set())}>
                清除选择
              </Button>
            </>
          )}
          {filterActive && (
            <Button
              size="xs"
              variant="subtle"
              leftSection={<IconFilterOff size={14} />}
              onClick={clearFilters}
            >
              重置筛选
            </Button>
          )}
        </Group>
      </Group>

      <Box visibleFrom="sm">{filtersContent}</Box>

      <Drawer
        opened={filterDrawerOpened}
        onClose={filterDrawer.close}
        title="筛选"
        position="right"
        hiddenFrom="sm"
      >
        {filtersContent}
      </Drawer>

      <PixiMediaGrid
        items={infinite.items}
        total={infinite.total}
        loading={infinite.loading}
        error={infinite.error}
        selectedIds={selectedIds}
        onSelect={handleSelect}
        onOpen={handleOpen}
        onNeedMore={infinite.loadMore}
        columns={columns}
      />

      <MediaDetailPanel
        files={infinite.items}
        initialIndex={detailIndex}
        onClose={() => setDetailIndex(null)}
        customImageExtensions={exts}
        onToggleFavorite={handleToggleFavorite}
        onFilterByTag={(tag) => {
          setTagId(tag.id);
          setDetailIndex(null);
        }}
      />

      <BatchActionsModals state={batch.modalState} />

      <ConfirmModal
        opened={pendingDelete.length > 0}
        title="删除媒体"
        message={`确定删除选中的 ${pendingDelete.length} 项？删除后进入回收站。`}
        confirmLabel="删除"
        onConfirm={() => void confirmDelete()}
        onCancel={() => setPendingDelete([])}
        loading={deleting}
      />
    </Stack>
  );
}
