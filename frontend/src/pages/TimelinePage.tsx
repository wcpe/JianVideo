import { useState, useCallback, useMemo } from 'react'
import { Stack, Title, TextInput, Group, SegmentedControl, Text, ActionIcon, Tooltip, Button, Drawer, Box } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { notifications } from '@mantine/notifications'
import { IconSearch, IconRefresh, IconFilter } from '@tabler/icons-react'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useInfiniteMedia } from '@/hooks/useInfiniteMedia'
import { useScanProgress } from '@/hooks/useScanProgress'
import TimelineView from '@/components/TimelineView'
import MediaFilterBar from '@/components/MediaFilterBar'
import MediaQueryFilters from '@/components/MediaQueryFilters'
import ContinueWatching from '@/components/ContinueWatching'
import OnThisDay from '@/components/OnThisDay'
import MediaDetailPanel from '@/components/MediaDetailPanel'
import ConfirmModal from '@/components/ConfirmModal'
import BatchActionsModals from '@/components/BatchActionsModals'
import { useBatchActions } from '@/hooks/useBatchActions'
import { extractErrorMessage } from '@/utils/error'
import * as libApi from '@/api/library'
import type { TimelineGranularity } from '@/utils/timeline'
import type { MediaFile } from '@/types'

/** 时间轴页：按日期分组的时间线浏览，虚拟滚动 + 滚动加载更多 */
export default function TimelinePage() {
  // 文件详情面板（FR-34）：选中项下标，null 表示关闭
  const [detailIndex, setDetailIndex] = useState<number | null>(null)
  // 收藏/标签筛选（FR-41）
  const [favorite, setFavorite] = useState(false)
  const [tagId, setTagId] = useState(0)
  // 结构化筛选（FR-36）：类型 / 最小大小 / 拍摄时间范围
  const [mediaType, setMediaType] = useState<'' | 'image' | 'video'>('')
  const [sizeMin, setSizeMin] = useState(0)
  const [timeFrom, setTimeFrom] = useState('')
  const [timeTo, setTimeTo] = useState('')
  // 时间轴缩放粒度（FR-32）：按媒体时间组织，日/月/年缩放
  const [granularity, setGranularity] = useState<TimelineGranularity>('day')
  // 批量删除（FR-69）：待确认删除 id 列表（非空即弹确认框）+ 删除进行中
  const [pendingDelete, setPendingDelete] = useState<number[]>([])
  const [deleting, setDeleting] = useState(false)
  // 移动端筛选抽屉开合（FR-86）：窄屏将筛选控件收进抽屉，搜索框常驻
  const [filterDrawerOpened, filterDrawer] = useDisclosure(false)
  const infinite = useInfiniteMedia({ favorite, tagId, mediaType, sizeMin, timeFrom, timeTo, sort: 'media_time' })
  // 批量操作（FR-91）：加相册 / 打标签 / 打包下载
  const batch = useBatchActions()
  const paths = useLibraryPaths(undefined)
  const exts = paths.customImageExtensions
  // 扫描完成后重载第一页
  useScanProgress(() => infinite.reload())

  // 点击媒体（图片与视频统一）打开文件详情面板（FR-34）
  const handleOpen = useCallback((f: MediaFile) => {
    const i = infinite.items.findIndex(x => x.id === f.id)
    if (i >= 0) setDetailIndex(i)
  }, [infinite.items])

  // 搜索/筛选是否生效（FR-98）：用于区分「无结果」与「空库」空态
  const filterActive = useMemo(
    () => !!(infinite.searchInput.trim() || favorite || tagId || mediaType || sizeMin || timeFrom || timeTo),
    [infinite.searchInput, favorite, tagId, mediaType, sizeMin, timeFrom, timeTo],
  )

  // 清除全部筛选（FR-98）：无结果态「清除筛选」CTA 调用
  const clearFilters = useCallback(() => {
    infinite.setSearchInput('')
    setFavorite(false)
    setTagId(0)
    setMediaType('')
    setSizeMin(0)
    setTimeFrom('')
    setTimeTo('')
  }, [infinite])

  // 切换收藏（FR-41）：调用接口后刷新列表
  const handleToggleFavorite = useCallback(async (f: MediaFile) => {
    try {
      await libApi.setMediaFavorite(f.id, !f.favorite)
      infinite.reload()
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '更新收藏失败') })
    }
  }, [infinite])

  // 执行批量软删（FR-69）：确认后调端点，成功刷新 + 关闭确认框
  const confirmDelete = useCallback(async () => {
    setDeleting(true)
    try {
      const n = await libApi.batchDeleteMediaFiles(pendingDelete)
      notifications.show({ color: 'green', message: `已删除 ${n} 项到回收站` })
      setPendingDelete([])
      infinite.reload()
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '批量删除失败') })
    } finally {
      setDeleting(false)
    }
  }, [pendingDelete, infinite])

  // 筛选控件（FR-86）：桌面内联铺开、移动收进抽屉，二者共用同一套受控状态
  const filtersContent = (
    <Stack gap="md">
      {/* 收藏与标签筛选（FR-41） */}
      <MediaFilterBar
        favorite={favorite}
        onFavoriteChange={setFavorite}
        tagId={tagId}
        onTagIdChange={setTagId}
      />

      {/* 结构化筛选（FR-36）：类型 / 大小 / 拍摄时间范围 */}
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

      {/* 时间轴缩放（FR-32）：按媒体时间组织，日/月/年粒度切换 */}
      <Group gap="xs" align="center">
        <Text size="xs" c="dimmed">缩放</Text>
        <SegmentedControl
          aria-label="时间轴缩放"
          size="xs"
          value={granularity}
          onChange={(v) => setGranularity(v as TimelineGranularity)}
          data={[
            { value: 'day', label: '日' },
            { value: 'month', label: '月' },
            { value: 'year', label: '年' },
          ]}
        />
      </Group>
    </Stack>
  )

  return (
    <Stack gap="md">
      <Group justify="space-between" align="center">
        <Title order={2}>时间轴</Title>
        {/* 手动刷新（FR-67）：重载首页数据 */}
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
      </Group>

      {/* 继续观看（FR-44）：有进度未看完的媒体，空列表时自动隐藏 */}
      <ContinueWatching />

      {/* 那年今日（FR-72）：往年同一天拍摄的媒体回忆，空列表时自动隐藏 */}
      <OnThisDay />

      {/* 搜索（FR-35 表达式：ext: / type: / size: / 裸词）：移动端亦常驻不收进抽屉（FR-86） */}
      <Group gap="xs" align="center" wrap="nowrap">
        <TextInput
          placeholder="搜索：文件名，或 ext:jpg type:image size:>10mb"
          leftSection={<IconSearch size={14} />}
          value={infinite.searchInput}
          onChange={(e) => infinite.setSearchInput(e.target.value)}
          size="sm"
          style={{ flex: 1 }}
        />
        {/* 移动端「筛选」入口（FR-86）：打开抽屉，桌面隐藏 */}
        <Button
          variant="default"
          size="sm"
          leftSection={<IconFilter size={16} />}
          onClick={filterDrawer.open}
          hiddenFrom="sm"
        >
          筛选
        </Button>
      </Group>

      {/* 桌面端筛选区内联铺开（FR-86）：窄屏隐藏，改由抽屉承载 */}
      <Box visibleFrom="sm">{filtersContent}</Box>

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
        {filtersContent}
      </Drawer>

      <TimelineView
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

      <BatchActionsModals state={batch.modalState} />

      <MediaDetailPanel
        files={infinite.items}
        initialIndex={detailIndex}
        onClose={() => setDetailIndex(null)}
        customImageExtensions={exts}
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
  )
}
