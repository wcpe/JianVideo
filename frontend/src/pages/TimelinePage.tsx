import { useState, useCallback, useMemo, useRef, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Stack,
  TextInput,
  Group,
  SegmentedControl,
  Text,
  ActionIcon,
  Tooltip,
  Button,
  Drawer,
  Box,
  NativeSelect,
} from '@mantine/core';
import { useDisclosure, useMediaQuery } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import {
  IconRefresh,
  IconFilter,
  IconCalendarSearch,
  IconFilterOff,
} from '@tabler/icons-react';
import { useLibraryPaths } from '@/hooks/useLibraryPaths';
import { useInfiniteMedia } from '@/hooks/useInfiniteMedia';
import { useScanProgress } from '@/hooks/useScanProgress';
import TimelineView, { type TimelineViewHandle } from '@/components/TimelineView';
import CategoryFilter from '@/components/CategoryFilter';
import LibraryFilter from '@/components/LibraryFilter';
import MediaQueryFilters from '@/components/MediaQueryFilters';
import ContinueWatching from '@/components/ContinueWatching';
import OnThisDay from '@/components/OnThisDay';
import RecentlyViewed from '@/components/RecentlyViewed';
import MediaDetailPanel from '@/components/MediaDetailPanel';
import PageHeader from '@/components/PageHeader';
import PullToRefresh from '@/components/PullToRefresh';
import ConfirmModal from '@/components/ConfirmModal';
import BatchActionsModals from '@/components/BatchActionsModals';
import { useBatchActions } from '@/hooks/useBatchActions';
import { extractErrorMessage } from '@/utils/error';
import * as libApi from '@/api/library';
import type { TimelineGranularity } from '@/utils/timeline';
import type { MediaFile } from '@/types';

/**
 * 规范化 ?date= 查询为合法 YYYY-MM-DD（FR-145）：仅接受严格 YYYY-MM-DD 且为真实日历日；
 * 非法（格式不符 / 越界 / 溢出如 2 月 30 日）返回空串。纯函数。
 */
function normalizeDayQuery(raw: string | null): string {
  const s = (raw ?? '').trim();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return '';
  const [y, m, d] = s.split('-').map(Number);
  const dt = new Date(y, m - 1, d);
  // 回读校验：溢出日期（如 2-30）会被规整到别的日子，比对拒绝
  if (dt.getFullYear() !== y || dt.getMonth() !== m - 1 || dt.getDate() !== d) return '';
  return s;
}

/**
 * 取某日的次日 YYYY-MM-DD（FR-145）：当天筛选上界用（后端 <= 次日零点即含当天整天）。
 * 入参须为合法 YYYY-MM-DD；非法返回空串。纯函数。
 */
function nextDay(day: string): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return '';
  const [y, m, d] = day.split('-').map(Number);
  const dt = new Date(y, m - 1, d + 1);
  return `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`;
}

/** 时间轴页：按日期分组的时间线浏览，虚拟滚动 + 滚动加载更多 */
export default function TimelinePage() {
  // 搜索统一到页眉全局搜索（FR-132）：搜索唯一入口为页眉，时间轴页不再有独立搜索框。
  // 从 URL ?search= 取初值供首屏请求；页眉改写 ?search= 后经下方 effect 联动驱动本页筛选。
  // 回忆卡片跳「那天」（FR-145）：从 URL ?date=YYYY-MM-DD 取初值，首屏即按当天筛选并滚动到该日分组。
  const [searchParams] = useSearchParams();
  const urlSearch = searchParams.get('search') ?? '';
  const initialSearch = urlSearch;
  const initialDate = normalizeDayQuery(searchParams.get('date'));
  // 文件详情面板（FR-34）：选中项下标，null 表示关闭
  const [detailIndex, setDetailIndex] = useState<number | null>(null);
  // 收藏/标签筛选（FR-41）
  const [favorite, setFavorite] = useState(false);
  const [tagId, setTagId] = useState(0);
  // 媒体库（图库）筛选（FR-144）：0 表示全部图库不过滤，>0 仅含该库媒体
  const [libraryId, setLibraryId] = useState(0);
  // 结构化筛选（FR-36）：类型 / 最小大小 / 拍摄时间范围
  const [mediaType, setMediaType] = useState<'' | 'image' | 'video'>('');
  const [sizeMin, setSizeMin] = useState(0);
  // 本地影视信息筛选（FR2-031）：全局查看自动、人工或尚未推断的媒体
  const [inferenceStatus, setInferenceStatus] = useState<'' | 'auto' | 'manual' | 'missing'>('');
  // 拍摄时间范围（FR-36 + FR-145）：从 ?date= 初始化为当天区间——下界当天零点、上界次日零点
  // （后端 <= 次日零点即含当天整天）；无 ?date= 时为空、不过滤。
  const [timeFrom, setTimeFrom] = useState(initialDate);
  const [timeTo, setTimeTo] = useState(initialDate ? nextDay(initialDate) : '');
  // 时间轴缩放粒度（FR-32）：按媒体时间组织，日/月/年缩放
  const [granularity, setGranularity] = useState<TimelineGranularity>('day');
  // 时间轴命令式句柄（FR-142）：日期跳转 + 粒度切换视口锁定
  const timelineRef = useRef<TimelineViewHandle>(null);
  // 「跳转到日期」输入（FR-142）：YYYY / YYYY-MM / YYYY-MM-DD
  const [jumpDate, setJumpDate] = useState('');
  // 粒度切换视口锁定（FR-142）：切换前记录视口顶部分组日期，切换后据其重新定位，避免视口漂移
  const lockDateRef = useRef<string>('');
  // 批量删除（FR-69）：待确认删除 id 列表（非空即弹确认框）+ 删除进行中
  const [pendingDelete, setPendingDelete] = useState<number[]>([]);
  const [deleting, setDeleting] = useState(false);
  // 移动端筛选抽屉开合（FR-86）：窄屏将筛选控件收进抽屉，搜索框常驻
  const [filterDrawerOpened, filterDrawer] = useDisclosure(false);
  // 窄屏判断（FR-143）：仅窄屏启用下拉刷新；取 Mantine sm 断点（48em），SSR/未知回退桌面态
  const isNarrow = useMediaQuery('(max-width: 48em)') ?? false;
  const infinite = useInfiniteMedia({
    favorite,
    tagId,
    libraryId,
    mediaType,
    sizeMin,
    timeFrom,
    timeTo,
    inference: inferenceStatus,
    sort: 'media_time',
    initialSearch,
  });
  // 页眉全局搜索联动（FR-132）：页眉改写 URL ?search= 后，把新关键词同步到本页搜索态，
  // 驱动时间轴按新词重新请求；与已挂载页联动（不再依赖页内搜索框）。仅在 URL search 变化时同步。
  useEffect(() => {
    infinite.setSearchInput(urlSearch);
    // 仅随 URL search 变化联动；setSearchInput 稳定，无需入依赖以免每次渲染重置用户态
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlSearch]);

  // 批量操作（FR-91）：加相册 / 打标签 / 打包下载
  const batch = useBatchActions();
  const paths = useLibraryPaths(undefined);
  const exts = paths.customImageExtensions;
  // 扫描完成后重载第一页
  useScanProgress(() => infinite.reload());

  // 点击媒体（图片与视频统一）打开文件详情面板（FR-34）
  // 同时记录最近查看（FR-120）：打开即标记 last_viewed_at，失败静默不阻塞打开
  const handleOpen = useCallback(
    (f: MediaFile) => {
      const i = infinite.items.findIndex((x) => x.id === f.id);
      if (i >= 0) setDetailIndex(i);
      void libApi.setMediaViewed(f.id).catch(() => {});
    },
    [infinite.items],
  );

  // 搜索/筛选是否生效（FR-98）：用于区分「无结果」与「空库」空态
  const filterActive = useMemo(
    () =>
      !!(
        infinite.searchInput.trim() ||
        favorite ||
        tagId ||
        libraryId ||
        mediaType ||
        sizeMin ||
        inferenceStatus ||
        timeFrom ||
        timeTo
      ),
    [
      infinite.searchInput,
      favorite,
      tagId,
      libraryId,
      mediaType,
      sizeMin,
      inferenceStatus,
      timeFrom,
      timeTo,
    ],
  );

  // 清除全部筛选（FR-98）：无结果态「清除筛选」CTA 调用
  const clearFilters = useCallback(() => {
    infinite.setSearchInput('');
    setFavorite(false);
    setTagId(0);
    setLibraryId(0);
    setMediaType('');
    setSizeMin(0);
    setInferenceStatus('');
    setTimeFrom('');
    setTimeTo('');
  }, [infinite]);

  // 跳转到日期（FR-142）：解析输入定位到对应分组；非法/无命中时提示不跳转
  const handleJumpToDate = useCallback(() => {
    const query = jumpDate.trim();
    if (!query) return;
    const ok = timelineRef.current?.scrollToDate(query) ?? false;
    if (!ok) {
      notifications.show({
        color: 'yellow',
        message: '无法识别该日期，请输入 YYYY、YYYY-MM 或 YYYY-MM-DD',
      });
    }
  }, [jumpDate]);

  // 切换缩放粒度（FR-142 视口锁定）：切换前记录视口顶部分组日期，切换后由 effect 重新定位。
  // 「所有」粒度为单组、锁定无意义，故不记录（lockDateRef 置空）。
  const handleGranularityChange = useCallback((next: TimelineGranularity) => {
    lockDateRef.current =
      next === 'all' ? '' : (timelineRef.current?.getTopVisibleGroupDate() ?? '');
    setGranularity(next);
  }, []);

  // 粒度变化落地后据切换前记录的视口日期重新定位（FR-142），消费一次即清空避免重复跳转。
  useEffect(() => {
    const date = lockDateRef.current;
    if (!date) return;
    lockDateRef.current = '';
    timelineRef.current?.scrollToDate(date);
  }, [granularity]);

  // ?date= 跳「那天」（FR-145）：首屏数据到位后滚动到该日分组，仅执行一次（ref 消费标记避免重复）。
  const jumpConsumedRef = useRef(false);
  useEffect(() => {
    if (!initialDate || jumpConsumedRef.current) return;
    if (infinite.items.length === 0) return;
    jumpConsumedRef.current = true;
    timelineRef.current?.scrollToDate(initialDate);
  }, [initialDate, infinite.items.length]);

  // 切换收藏（FR-41）：调用接口后刷新列表
  const handleToggleFavorite = useCallback(
    async (f: MediaFile) => {
      try {
        await libApi.setMediaFavorite(f.id, !f.favorite);
        infinite.reload();
      } catch (err) {
        notifications.show({ color: 'red', message: extractErrorMessage(err, '更新收藏失败') });
      }
    },
    [infinite],
  );

  // 执行批量软删（FR-69）：确认后调端点，成功刷新 + 关闭确认框
  const confirmDelete = useCallback(async () => {
    setDeleting(true);
    try {
      const n = await libApi.batchDeleteMediaFiles(pendingDelete);
      notifications.show({ color: 'green', message: `已删除 ${n} 项到回收站` });
      setPendingDelete([]);
      infinite.reload();
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '批量删除失败') });
    } finally {
      setDeleting(false);
    }
  }, [pendingDelete, infinite]);

  // 筛选控件（FR-86）：桌面内联铺开、移动收进抽屉，二者共用同一套受控状态
  // 控件按「内容筛选 | 视图」分组成带标签容器（FR-100），更清晰；搜索框在本块之外常驻（FR-35/FR-86）。
  const filtersContent = (
    <Stack gap="lg">
      {/* 内容筛选组（FR-100）：收藏/标签（FR-41）+ 类型/大小/拍摄时间（FR-36） */}
      <Box aria-label="内容筛选" role="group">
        <Text size="xs" fw={600} c="dimmed" tt="uppercase" mb={6}>
          内容筛选
        </Text>
        <Stack gap="sm">
          <CategoryFilter
            favorite={favorite}
            onFavoriteChange={setFavorite}
            tagId={tagId}
            onTagIdChange={setTagId}
          />
          {/* 媒体库（图库）筛选（FR-144）：按图库过滤，复用后端 library_id；含多选模式入口 */}
          <LibraryFilter libraryId={libraryId} onLibraryIdChange={setLibraryId} />
          <MediaQueryFilters
            mediaType={mediaType}
            onMediaTypeChange={setMediaType}
            sizeMin={sizeMin}
            onSizeMinChange={setSizeMin}
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
              setInferenceStatus(e.currentTarget.value as '' | 'auto' | 'manual' | 'missing')
            }
            data={[
              { value: '', label: '全部影视信息' },
              { value: 'auto', label: '自动推断' },
              { value: 'manual', label: '人工纠正' },
              { value: 'missing', label: '待推断' },
            ]}
          />
        </Stack>
      </Box>

      {/* 视图组（FR-100）：时间轴缩放（FR-32 + FR-120）年/月/日/所有粒度切换 + 日期跳转（FR-142） */}
      <Box aria-label="视图" role="group">
        <Text size="xs" fw={600} c="dimmed" tt="uppercase" mb={6}>
          视图
        </Text>
        <Group gap="sm" align="center" wrap="wrap">
          <Group gap="xs" align="center">
            <Text size="xs" c="dimmed">
              缩放
            </Text>
            <SegmentedControl
              aria-label="时间轴缩放"
              size="xs"
              value={granularity}
              // 粒度切换走视口锁定（FR-142）：切换前记录视口日期，切换后重新定位
              onChange={(v) => handleGranularityChange(v as TimelineGranularity)}
              data={[
                { value: 'year', label: '年' },
                { value: 'month', label: '月' },
                { value: 'day', label: '日' },
                { value: 'all', label: '所有' },
              ]}
            />
          </Group>
          {/* 跳转到日期（FR-142）：输入 YYYY / YYYY-MM / YYYY-MM-DD，回车定位到对应分组 */}
          <TextInput
            aria-label="跳转到日期"
            placeholder="跳转：2025 / 2025-08 / 2025-08-15"
            leftSection={<IconCalendarSearch size={14} />}
            size="xs"
            value={jumpDate}
            onChange={(e) => setJumpDate(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                handleJumpToDate();
              }
            }}
            style={{ width: 220 }}
          />
        </Group>
      </Box>
    </Stack>
  );

  return (
    <Stack gap="md">
      <PageHeader
        title="时间轴"
        actions={
          // 手动刷新（FR-67）：重载首页数据
          <Tooltip label="刷新">
            <ActionIcon
              variant="default"
              size="lg"
              aria-label="刷新"
              loading={infinite.loading}
              onClick={() => infinite.reload()}
            >
              <IconRefresh size={18} />
            </ActionIcon>
          </Tooltip>
        }
      />

      {/* 工具栏冻结吸顶（FR-139）：搜索 + 内容筛选 + 视图聚合为顶部工具栏，滚动时 sticky 吸顶。
          页面滚动容器为 window，吸顶基准取 AppShell 头部偏移；半透明底色让下方内容滚过时不穿透。 */}
      <Box
        data-testid="timeline-toolbar"
        style={{
          position: 'sticky',
          top: 'var(--app-shell-header-offset, 0px)',
          zIndex: 3,
          background: 'var(--mantine-color-body)',
          paddingBottom: 'var(--mantine-spacing-sm)',
        }}
      >
        <Stack gap="sm">
          {/* 移动端「筛选」入口（FR-86）：搜索已统一到页眉，页内不再有搜索框；窄屏保留筛选抽屉入口，桌面隐藏 */}
          <Group justify="flex-end" hiddenFrom="sm">
            <Button
              variant="default"
              size="sm"
              leftSection={<IconFilter size={16} />}
              onClick={filterDrawer.open}
            >
              筛选
            </Button>
          </Group>

          {/* 桌面端筛选区内联铺开（FR-86）：窄屏隐藏，改由抽屉承载 */}
          <Box visibleFrom="sm">{filtersContent}</Box>
        </Stack>
      </Box>

      {/* 移动端筛选抽屉（FR-86）：承载与桌面同一套受控筛选控件 */}
      <Drawer
        opened={filterDrawerOpened}
        onClose={filterDrawer.close}
        title="筛选"
        position="right"
        padding="md"
        hiddenFrom="sm"
        closeButtonProps={{ 'aria-label': '关闭筛选' }}
      >
        <Stack gap="lg">
          {filtersContent}
          {/* 抽屉底部一键重置（FR-143）：窄屏逐项清空筛选繁琐，提供整体重置入口；
              无筛选生效时禁用，重置后关闭抽屉回到全部结果 */}
          <Button
            variant="light"
            color="gray"
            leftSection={<IconFilterOff size={16} />}
            disabled={!filterActive}
            onClick={() => {
              clearFilters();
              filterDrawer.close();
            }}
          >
            重置筛选
          </Button>
        </Stack>
      </Drawer>

      {/* 继续观看（FR-44）：有进度未看完的媒体，空列表时自动隐藏 */}
      <ContinueWatching />

      {/* 最近查看（FR-120）：最近打开过的媒体回忆，空列表时自动隐藏 */}
      <RecentlyViewed />

      {/* 那年今日（FR-72）：往年同一天拍摄的媒体回忆，空列表时自动隐藏 */}
      <OnThisDay />

      {/* 下拉刷新（FR-143）：仅窄屏启用，页面顶部下拉松手重载首屏；桌面（enabled=false）纯透传 */}
      <PullToRefresh enabled={isNarrow} onRefresh={() => infinite.reload()}>
        <TimelineView
          ref={timelineRef}
          mediaFiles={infinite.items}
          loading={infinite.loading && infinite.items.length === 0}
          error={infinite.error}
          customImageExtensions={exts}
          onErrorClose={() => infinite.setError(null)}
          onOpenFile={handleOpen}
          onToggleFavorite={handleToggleFavorite}
          onLoadMore={infinite.loadMore}
          hasMore={infinite.hasMore}
          loadingMore={infinite.loading && infinite.items.length > 0}
          granularity={granularity}
          filtered={filterActive}
          onClearFilter={clearFilters}
          onBatchDelete={(ids) => setPendingDelete(ids)}
          onDeleteOne={(f) => setPendingDelete([f.id])}
          onBatchAddToAlbum={batch.openAddToAlbum}
          onBatchAddTag={batch.openAddTag}
          onBatchDownload={batch.download}
        />
      </PullToRefresh>

      <BatchActionsModals state={batch.modalState} />

      <MediaDetailPanel
        files={infinite.items}
        initialIndex={detailIndex}
        onClose={() => setDetailIndex(null)}
        customImageExtensions={exts}
        onToggleFavorite={handleToggleFavorite}
      />

      {/* 批量删除二次确认（FR-69）：删除进回收站，可在回收站还原 */}
      <ConfirmModal
        opened={pendingDelete.length > 0}
        title="删除媒体"
        message={`确定删除选中的 ${pendingDelete.length} 项？删除后进入回收站，可在回收站还原。`}
        confirmLabel="删除"
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete([])}
        loading={deleting}
      />
    </Stack>
  );
}
