import {
  useRef,
  useEffect,
  useState,
  useMemo,
  useCallback,
  useImperativeHandle,
  forwardRef,
} from 'react';
import {
  SimpleGrid,
  Card,
  Text,
  Group,
  Box,
  Skeleton,
  Alert,
  Stack,
  Loader,
  Center,
  ActionIcon,
  Checkbox,
} from '@mantine/core';
import {
  IconFolder,
  IconAlertCircle,
  IconStar,
  IconStarFilled,
  IconSearchOff,
  IconPlayerPlay,
  IconDots,
  IconCamera,
  IconMapPin,
  IconChecks,
} from '@tabler/icons-react';
import EmptyState from '@/components/EmptyState';
import { useWindowVirtualizer } from '@tanstack/react-virtual';
import { isImageFile, mediaDisplayName } from '@/utils/media';
import {
  groupMediaByDate,
  resolveDateToGroupIndex,
  groupDateAtIndex,
  summarizeGroup,
} from '@/utils/timeline';
import { useElementWidth } from '@/hooks/useElementWidth';
import {
  gridColumnsForWidth,
  thumbnailSizeForColumn,
  buildTimelineRows,
} from '@/utils/timelineGrid';
import MediaThumbnail from '@/components/MediaThumbnail';
import MediaCardOverlay from '@/components/MediaCardOverlay';
import { useMediaQuery } from '@mantine/hooks';
import TimelineScrubber from '@/components/TimelineScrubber';
import MediaContextMenu, { type ContextMenuState } from '@/components/MediaContextMenu';
import SelectionBatchBar from '@/components/SelectionBatchBar';
import { useMultiSelect, type ClickModifiers } from '@/hooks/useMultiSelect';
import type { MediaFile } from '@/types';
import type { DateGroup, TimelineGranularity } from '@/utils/timeline';

/**
 * 选择上下文（FR-69）：把多选状态与卡片交互回调下传给日期组渲染。
 * flatIndexOf 把 media id 映射到「全部已加载项展平后」的有序下标，供 Shift 区间等手势使用。
 */
interface SelectionContext {
  enabled: boolean;
  checkboxMode: boolean;
  isSelected: (id: number) => boolean;
  flatIndexOf: (id: number) => number;
  onCardClick: (index: number, mods: ClickModifiers) => void;
  onCardContextMenu: (id: number, e: React.MouseEvent) => void;
}

/**
 * 时间轴命令式句柄（FR-142）：供 TimelinePage 实现日期跳转与粒度切换视口锁定。
 * - scrollToDate：按日期查询（YYYY/-MM/-DD）滚动到对应分组，命中返回 true、非法/无命中返回 false。
 * - getTopVisibleGroupDate：返回当前视口顶部分组的日期键，用于粒度切换前记录视口位置。
 */
export interface TimelineViewHandle {
  scrollToDate(query: string): boolean;
  getTopVisibleGroupDate(): string;
}

interface TimelineViewProps {
  mediaFiles: MediaFile[];
  loading: boolean;
  error: string | null;
  customImageExtensions: Record<number, string[]>;
  onErrorClose: () => void;
  onOpenFile: (file: MediaFile) => void;
  // 切换收藏（FR-41）：传入时媒体卡片展示标星按钮
  onToggleFavorite?: (file: MediaFile) => void;
  // 滚动到底部时触发加载更多
  onLoadMore?: () => void;
  // 是否还有更多数据可加载
  hasMore?: boolean;
  // 是否正在加载下一页（用于底部提示）
  loadingMore?: boolean;
  // 分组粒度（FR-32 缩放）：日 / 月 / 年，默认日
  granularity?: TimelineGranularity;
  // 搜索/筛选生效标记（FR-98）：为真时 0 结果显示「无匹配结果」而非「空库」
  filtered?: boolean;
  // 清除筛选回调（FR-98）：无结果态「清除筛选」CTA，传入才渲染
  onClearFilter?: () => void;
  // 多选与批量删除（FR-69），均可选；传入任一即启用选择手势与右键菜单
  onSelectionChange?: (ids: number[]) => void;
  onBatchDelete?: (ids: number[]) => void;
  onDeleteOne?: (file: MediaFile) => void;
  // 批量操作（FR-91 / FR2-053）：以 id 集为对象（无选中退化为右键项），均可选
  onBatchAddToAlbum?: (ids: number[]) => void;
  onBatchAddTag?: (ids: number[]) => void;
  onBatchDownload?: (ids: number[]) => void;
  onBatchTranscode?: (ids: number[]) => void;
  onBatchMove?: (ids: number[]) => void;
}

/** 分组头行预估高度（用于虚拟化初始测量，会被实际测量覆盖） */
const HEADER_ROW_ESTIMATE = 36;
/** 单行卡片预估高度（方形卡 + 间距，会被实际测量覆盖） */
const CARDS_ROW_ESTIMATE = 180;

/** 组级全选按钮文案（FR-146）：按粒度区分「选当天/当月全部」；年/所有粒度无组级全选。 */
const GROUP_SELECT_LABEL: Partial<Record<TimelineGranularity, string>> = {
  day: '选当天全部',
  month: '选当月全部',
};

/**
 * 渲染单个日期分组头（FR-138 + FR-146）：圆点 + 整串日期 + 聚合信息（数量 / 主要设备 / 地点），
 * 横向轻量标题。选择启用且为日/月粒度时，附「选当天/当月全部」组级全选入口（FR-146）。
 */
function DateGroupHeader({
  group,
  granularity,
  selectionEnabled,
  onSelectGroup,
}: {
  group: DateGroup;
  granularity: TimelineGranularity;
  selectionEnabled: boolean;
  onSelectGroup: (ids: number[]) => void;
}) {
  // 聚合数量 / 主要设备 / 地点（FR-146）；空维度不展示
  const summary = summarizeGroup(group);
  const selectLabel = GROUP_SELECT_LABEL[granularity];
  return (
    <Group gap={8} align="center" wrap="nowrap" data-timeline-group-header pt="lg" pb="xs">
      <Box
        style={{
          width: 8,
          height: 8,
          borderRadius: '50%',
          flexShrink: 0,
          background: 'var(--mantine-color-purple-6)',
        }}
      />
      <Text fw={600} size="sm" style={{ lineHeight: 1.1 }}>
        {group.date}
      </Text>
      <Text size="xs" c="dimmed">
        {summary.count} 项
      </Text>
      {/* 主要设备/相机（FR-146）：组内最常见 camera，无则不显示 */}
      {summary.camera && (
        <Group gap={2} align="center" wrap="nowrap" c="dimmed">
          <IconCamera size={13} />
          <Text size="xs" truncate maw={160}>
            {summary.camera}
          </Text>
        </Group>
      )}
      {/* 地点（FR-146 接线 FR-147）：组内最常见 location，无 GPS 则不显示 */}
      {summary.location && (
        <Group gap={2} align="center" wrap="nowrap" c="dimmed">
          <IconMapPin size={13} />
          <Text size="xs" truncate maw={160}>
            {summary.location}
          </Text>
        </Group>
      )}
      {/* 组级全选（FR-146）：日/月粒度下一键选中该日/该月全部已加载媒体 */}
      {selectionEnabled && selectLabel && (
        <ActionIcon
          variant="subtle"
          color="gray"
          size="sm"
          aria-label={selectLabel}
          title={selectLabel}
          onClick={() => onSelectGroup(group.files.map((f) => f.id))}
        >
          <IconChecks size={15} />
        </ActionIcon>
      )}
    </Group>
  );
}

/** 单张媒体卡（FR-99 + FR-140）：缩略图铺满 + 叠层信息 + 角部操作。 */
function MediaGridCard({
  file,
  isImage,
  requestSize,
  onOpenFile,
  onToggleFavorite,
  selection,
}: {
  file: MediaFile;
  isImage: boolean;
  requestSize: number;
  onOpenFile: (file: MediaFile) => void;
  onToggleFavorite?: (file: MediaFile) => void;
  selection: SelectionContext;
}) {
  const selected = selection.enabled && selection.isSelected(file.id);
  // 时间轴沿用「普通单击=打开」的画廊式交互；多选仅在带修饰键（Ctrl/Cmd/Shift）或复选框模式下触发。
  const handleClick = (e: React.MouseEvent) => {
    const isSelectGesture = e.ctrlKey || e.metaKey || e.shiftKey || selection.checkboxMode;
    if (selection.enabled && isSelectGesture) {
      selection.onCardClick(selection.flatIndexOf(file.id), e);
    } else {
      onOpenFile(file);
    }
  };
  return (
    <Card
      withBorder
      p={0}
      radius="md"
      style={{
        cursor: 'pointer',
        borderColor: selected ? 'var(--mantine-color-purple-6)' : undefined,
        overflow: 'hidden',
      }}
      onClick={handleClick}
      onContextMenu={(e) => selection.onCardContextMenu(file.id, e)}
      className="hover-card media-card"
      data-selected={selected || undefined}
    >
      {/* 缩略图铺满卡面 + 叠层信息（FR-99）：信息密度叠到底部渐变层 */}
      <Box style={{ position: 'relative' }}>
        <MediaThumbnail
          mediaID={file.id}
          fileName={file.file_name}
          aspectRatio="1"
          objectFit="cover"
          requestSize={requestSize}
          overlay={
            <MediaCardOverlay
              file={file}
              isImage={isImage}
              selected={selected}
              checkboxMode={selection.checkboxMode}
            />
          }
        />
        {selection.enabled && selection.checkboxMode && (
          <Checkbox
            size="xs"
            checked={selected}
            readOnly
            tabIndex={-1}
            aria-label={`选择 ${mediaDisplayName(file)}`}
            style={{ position: 'absolute', top: 6, left: 6, zIndex: 5 }}
          />
        )}
        {/* 左下角常驻收藏（FR-140）：常显可点的收藏按钮，复用 favorite API。 */}
        {onToggleFavorite && (
          <ActionIcon
            variant="filled"
            color={file.favorite ? 'yellow' : 'dark'}
            size="sm"
            radius="xl"
            aria-label={file.favorite ? '取消收藏' : '收藏'}
            title={file.favorite ? '取消收藏' : '收藏'}
            onClick={(e) => {
              e.stopPropagation();
              onToggleFavorite(file);
            }}
            style={{ position: 'absolute', left: 6, bottom: 6, zIndex: 6 }}
          >
            {file.favorite ? <IconStarFilled size={14} /> : <IconStar size={14} />}
          </ActionIcon>
        )}
        {/* hover 快捷操作浮层（FR-99）：默认隐藏，卡片悬停显现；播放 / 更多 */}
        <Group
          gap={6}
          wrap="nowrap"
          className="media-card-actions"
          style={{ position: 'absolute', top: 6, right: 6, zIndex: 6 }}
        >
          <ActionIcon
            variant="filled"
            color="dark"
            size="sm"
            radius="xl"
            aria-label="播放"
            title="播放"
            onClick={(e) => {
              e.stopPropagation();
              onOpenFile(file);
            }}
          >
            <IconPlayerPlay size={14} />
          </ActionIcon>
          {selection.enabled && (
            <ActionIcon
              variant="filled"
              color="dark"
              size="sm"
              radius="xl"
              aria-label="更多操作"
              title="更多操作"
              onClick={(e) => {
                e.stopPropagation();
                selection.onCardContextMenu(file.id, e);
              }}
            >
              <IconDots size={14} />
            </ActionIcon>
          )}
        </Group>
      </Box>
    </Card>
  );
}

/**
 * 时间轴视图：按日期分组后展平为「头行 + 网格行」，对一维行列表做窗口虚拟化（FR-141）。
 * 卡片级虚拟化以网格行为单元，单日上千卡片也只渲染视口附近若干行；列数与缩略图尺寸随容器宽度自适应。
 * 经 forwardRef 暴露日期跳转与视口锁定句柄（FR-142）。
 */
function TimelineViewInner(
  {
    mediaFiles,
    loading,
    error,
    customImageExtensions,
    onErrorClose,
    onOpenFile,
    onToggleFavorite,
    onLoadMore,
    hasMore,
    loadingMore,
    granularity = 'day',
    filtered = false,
    onClearFilter,
    onSelectionChange,
    onBatchDelete,
    onDeleteOne,
    onBatchAddToAlbum,
    onBatchAddTag,
    onBatchDownload,
    onBatchTranscode,
    onBatchMove,
  }: TimelineViewProps,
  ref: React.Ref<TimelineViewHandle>,
) {
  // 列表容器 ref：既用于计算窗口虚拟化的 scrollMargin，又用于按真实宽度推导网格列数（FR-141）。
  const listRef = useRef<HTMLDivElement>(null);
  const [scrollMargin, setScrollMargin] = useState(0);
  const containerWidth = useElementWidth(listRef);
  // 窄屏判断（FR-143）：窄屏时间轴标尺切换为防误触的年份快速导航形态（compact）。
  // 取 Mantine sm 断点（48em）；SSR/未知时回退桌面态。
  const isNarrow = useMediaQuery('(max-width: 48em)') ?? false;

  // 分组结果记忆化：既稳定下方 useMemo 的依赖，又避免每次渲染重复分组
  const groups = useMemo(
    () => (error || loading ? [] : groupMediaByDate(mediaFiles, granularity)),
    [error, loading, mediaFiles, granularity],
  );

  // 网格列数与缩略图请求尺寸（FR-141）：按容器宽度推导列数，再按单卡列宽 + DPR 选缩略图档。
  const columns = gridColumnsForWidth(containerWidth);
  const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
  // 单卡列宽 ≈ 容器宽 / 列数（忽略间距的近似已足够选档）；宽度未测得时用 0，由 thumbnailSizeForColumn 兜底。
  const columnWidth = containerWidth > 0 ? containerWidth / columns : 0;
  const requestSize = thumbnailSizeForColumn(columnWidth, dpr);

  // 展平为头行 + 网格行（FR-141）：列数变化即重切，卡片级虚拟化以网格行为单元。
  const { rows, groupIndexToRow } = useMemo(
    () => buildTimelineRows(groups, columns),
    [groups, columns],
  );

  // 父组件关心选择（提供任一选择相关回调）时启用选择手势与右键菜单
  const selectionEnabled = !!(
    onSelectionChange ||
    onBatchDelete ||
    onDeleteOne ||
    onBatchAddToAlbum ||
    onBatchAddTag ||
    onBatchDownload ||
    onBatchTranscode ||
    onBatchMove
  );
  // 全部已加载项按分组渲染顺序展平为有序 id 列表——Ctrl+A / Shift 区间均以此为范围。
  const flatIds = useMemo(() => groups.flatMap((g) => g.files.map((f) => f.id)), [groups]);
  const flatIndex = useMemo(() => {
    const map = new Map<number, number>();
    flatIds.forEach((id, i) => map.set(id, i));
    return map;
  }, [flatIds]);
  const select = useMultiSelect(flatIds);
  const { selectedIds } = select;
  const [menu, setMenu] = useState<ContextMenuState | null>(null);

  // 选中集变化上抛父组件（升序）
  useEffect(() => {
    if (selectionEnabled) onSelectionChange?.(Array.from(selectedIds).sort((a, b) => a - b));
    // 仅在选中集变化时上抛
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIds]);

  // 右键媒体卡片：阻止系统菜单，记录光标位置与右键项 id（不改动选择集）
  function handleCardContextMenu(id: number, e: React.MouseEvent) {
    if (!selectionEnabled) return;
    e.preventDefault();
    setMenu({ x: e.clientX, y: e.clientY, targetId: id });
  }

  // 「删除选中」：有选中按选中批量删；无选中删右键项
  function handleMenuDelete(targetId: number) {
    setMenu(null);
    if (selectedIds.size > 0) {
      onBatchDelete?.(Array.from(selectedIds).sort((a, b) => a - b));
    } else {
      const f = mediaFiles.find((x) => x.id === targetId);
      if (f) onDeleteOne?.(f);
    }
  }

  // 批量操作（FR-91）：有选中按选中集；无选中退化为右键项。统一关菜单后回调。
  function runBatch(targetId: number, fn?: (ids: number[]) => void) {
    setMenu(null);
    const ids = selectedIds.size > 0 ? Array.from(selectedIds).sort((a, b) => a - b) : [targetId];
    fn?.(ids);
  }

  // sticky 批量条（FR-99）：以当前选中集为对象调用 FR-91 批量回调，复用已有多选 state。
  const selectedArr = () => Array.from(selectedIds).sort((a, b) => a - b);
  const runBarBatch = (fn?: (ids: number[]) => void) => {
    if (selectedIds.size > 0) fn?.(selectedArr());
  };

  const selection: SelectionContext = {
    enabled: selectionEnabled,
    checkboxMode: select.checkboxMode,
    isSelected: select.isSelected,
    flatIndexOf: (id) => flatIndex.get(id) ?? -1,
    onCardClick: select.handleItemClick,
    onCardContextMenu: handleCardContextMenu,
  };

  // 列表挂载/数据变化后测量容器距文档顶部的偏移作为 scrollMargin
  useEffect(() => {
    if (listRef.current) {
      setScrollMargin(listRef.current.getBoundingClientRect().top + window.scrollY);
    }
  }, [rows.length]);

  const virtualizer = useWindowVirtualizer({
    count: rows.length,
    estimateSize: (index) =>
      rows[index]?.type === 'header' ? HEADER_ROW_ESTIMATE : CARDS_ROW_ESTIMATE,
    overscan: 8,
    scrollMargin,
  });

  const virtualItems = virtualizer.getVirtualItems();

  // 按分组下标滚动到该组头行（FR-68 scrubber 与 FR-142 日期跳转共用）。
  const scrollToGroupIndex = useCallback(
    (groupIndex: number) => {
      const rowIndex = groupIndexToRow[groupIndex] ?? 0;
      virtualizer.scrollToIndex(rowIndex, { align: 'start' });
    },
    [groupIndexToRow, virtualizer],
  );

  // 命令式句柄（FR-142）：日期跳转 + 视口锁定。
  // - scrollToDate：纯函数 resolveDateToGroupIndex 把日期查询映射到分组下标后滚动；无命中返回 false。
  // - getTopVisibleGroupDate：取当前视口最靠上的虚拟行，二分定位其所属分组日期键。
  useImperativeHandle(
    ref,
    () => ({
      scrollToDate(query: string): boolean {
        const groupIndex = resolveDateToGroupIndex(groups, query);
        if (groupIndex < 0) return false;
        scrollToGroupIndex(groupIndex);
        return true;
      },
      getTopVisibleGroupDate(): string {
        // 取视口内最靠上的虚拟行下标；无虚拟项（未挂载/空）回退首组
        const items = virtualizer.getVirtualItems();
        const topRow = items.length > 0 ? items[0].index : 0;
        // 行下标 → 分组下标：groupIndexToRow 升序，取最后一个 <= topRow 的分组
        let groupIndex = 0;
        for (let g = 0; g < groupIndexToRow.length; g++) {
          if (groupIndexToRow[g] <= topRow) groupIndex = g;
          else break;
        }
        return groupDateAtIndex(groups, groupIndex);
      },
    }),
    [groups, groupIndexToRow, scrollToGroupIndex, virtualizer],
  );

  // 滚动加载更多：底部哨兵进入视口时触发 onLoadMore（IntersectionObserver，避免依赖 lastIndex 变化）。
  const sentinelRef = useRef<HTMLDivElement>(null);
  const loadMoreRef = useRef<() => void>(() => {});
  useEffect(() => {
    loadMoreRef.current = () => {
      if (hasMore && !loadingMore) onLoadMore?.();
    };
  }, [hasMore, loadingMore, onLoadMore]);
  useEffect(() => {
    const el = sentinelRef.current;
    // 无布局/无 IntersectionObserver 的环境（如测试 jsdom）降级为不挂载，避免崩溃
    if (!el || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) loadMoreRef.current();
      },
      { rootMargin: '400px' },
    );
    observer.observe(el);
    return () => observer.disconnect();
    // 列表在 loading/空 状态下不渲染哨兵，故依赖这些状态变化以在列表出现后重新挂载观察
  }, [loading, error, mediaFiles.length]);

  if (error) {
    return (
      <Alert
        icon={<IconAlertCircle size={16} />}
        color="red"
        withCloseButton
        onClose={onErrorClose}
      >
        {error}
      </Alert>
    );
  }

  if (loading) {
    return (
      <Stack gap="md">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} height={140} radius="md" />
        ))}
      </Stack>
    );
  }

  if (mediaFiles.length === 0) {
    // 区分「搜索/筛选无结果」与「空库」（FR-98）：前者给清除筛选引导
    return filtered ? (
      <EmptyState
        icon={
          <IconSearchOff
            size={72}
            stroke={1.2}
            style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }}
          />
        }
        title="没有匹配的媒体"
        description="当前搜索或筛选条件下没有结果，换个条件或清除筛选试试。"
        action={onClearFilter ? { label: '清除筛选', onClick: onClearFilter } : undefined}
      />
    ) : (
      <EmptyState
        icon={
          <IconFolder
            size={72}
            stroke={1.2}
            style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }}
          />
        }
        title="暂无媒体文件"
        description="在「存储库管理」添加目录并扫描后，媒体会在此按时间轴展示。"
      />
    );
  }

  return (
    <>
      {/* 虚拟化容器：高度撑满全部行总高，内部按虚拟项绝对定位 */}
      <Box ref={listRef} style={{ position: 'relative', height: virtualizer.getTotalSize() }}>
        {/* 可拖动时间 scrubber（FR-68）：拖动浮层预览、松手按分组跳转——映射到该组头行下标 */}
        <TimelineScrubber groups={groups} onSeek={scrollToGroupIndex} compact={isNarrow} />
        {virtualItems.map((virtualItem) => {
          const row = rows[virtualItem.index];
          if (!row) return null;
          return (
            <Box
              key={virtualItem.key}
              data-index={virtualItem.index}
              ref={virtualizer.measureElement}
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                // 减去 scrollMargin 修正窗口虚拟化的起始偏移
                transform: `translateY(${virtualItem.start - virtualizer.options.scrollMargin}px)`,
              }}
            >
              {row.type === 'header' ? (
                <DateGroupHeader
                  group={row.group}
                  granularity={granularity}
                  selectionEnabled={selectionEnabled}
                  onSelectGroup={select.selectIds}
                />
              ) : (
                <SimpleGrid
                  type="container"
                  cols={{ '180px': 3, '480px': 4, '760px': 5, '1040px': 6, '1360px': 8 }}
                  spacing="xs"
                  verticalSpacing="xs"
                  pb="xs"
                >
                  {row.files.map((file) => (
                    <MediaGridCard
                      key={file.id}
                      file={file}
                      isImage={isImageFile(file, customImageExtensions)}
                      requestSize={requestSize}
                      onOpenFile={onOpenFile}
                      onToggleFavorite={onToggleFavorite}
                      selection={selection}
                    />
                  ))}
                </SimpleGrid>
              )}
            </Box>
          );
        })}
      </Box>

      {/* 底部哨兵：进入视口（含 400px 提前量）即加载下一页 */}
      <div ref={sentinelRef} style={{ height: 1 }} />

      {/* 加载更多提示 */}
      {loadingMore && (
        <Center py="md">
          <Loader size="sm" color="purple" />
        </Center>
      )}

      {/* sticky 批量操作条（FR-99）：选中 ≥1 项时浮现，复用已有多选 state 与 FR-91 批量回调 */}
      {selectionEnabled && (
        <SelectionBatchBar
          count={selectedIds.size}
          onClear={select.clear}
          onDelete={() => onBatchDelete?.(selectedArr())}
          onAddToAlbum={onBatchAddToAlbum ? () => runBarBatch(onBatchAddToAlbum) : undefined}
          onAddTag={onBatchAddTag ? () => runBarBatch(onBatchAddTag) : undefined}
          onDownload={onBatchDownload ? () => runBarBatch(onBatchDownload) : undefined}
          onTranscode={onBatchTranscode ? () => runBarBatch(onBatchTranscode) : undefined}
          onMove={onBatchMove ? () => runBarBatch(onBatchMove) : undefined}
        />
      )}

      {/* 右键常用菜单（FR-69）：父组件关心选择时挂载 */}
      {selectionEnabled && (
        <MediaContextMenu
          state={menu}
          onClose={() => setMenu(null)}
          selectedCount={selectedIds.size}
          checkboxMode={select.checkboxMode}
          onDelete={handleMenuDelete}
          onSelectAll={() => {
            select.selectAll();
            setMenu(null);
          }}
          onInvert={() => {
            select.invertSelection();
            setMenu(null);
          }}
          onToggleCheckboxMode={() => {
            select.setCheckboxMode(!select.checkboxMode);
            setMenu(null);
          }}
          onAddToAlbum={onBatchAddToAlbum ? (id) => runBatch(id, onBatchAddToAlbum) : undefined}
          onAddTag={onBatchAddTag ? (id) => runBatch(id, onBatchAddTag) : undefined}
          onDownload={onBatchDownload ? (id) => runBatch(id, onBatchDownload) : undefined}
          onTranscode={onBatchTranscode ? (id) => runBatch(id, onBatchTranscode) : undefined}
          onMove={onBatchMove ? (id) => runBatch(id, onBatchMove) : undefined}
        />
      )}
    </>
  );
}

/** 时间轴视图：经 forwardRef 暴露 TimelineViewHandle（FR-142 日期跳转 / 视口锁定）。 */
const TimelineView = forwardRef<TimelineViewHandle, TimelineViewProps>(TimelineViewInner);
TimelineView.displayName = 'TimelineView';
export default TimelineView;
