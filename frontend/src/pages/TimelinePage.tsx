import { useState, useCallback } from 'react'
import { Stack, Title, TextInput, Group, SegmentedControl, Text, ActionIcon, Tooltip } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconSearch, IconRefresh } from '@tabler/icons-react'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useInfiniteMedia } from '@/hooks/useInfiniteMedia'
import { useScanProgress } from '@/hooks/useScanProgress'
import TimelineView from '@/components/TimelineView'
import MediaFilterBar from '@/components/MediaFilterBar'
import MediaQueryFilters from '@/components/MediaQueryFilters'
import ContinueWatching from '@/components/ContinueWatching'
import MediaDetailPanel from '@/components/MediaDetailPanel'
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
  const infinite = useInfiniteMedia({ favorite, tagId, mediaType, sizeMin, timeFrom, timeTo, sort: 'media_time' })
  const paths = useLibraryPaths(undefined)
  const exts = paths.customImageExtensions
  // 扫描完成后重载第一页
  useScanProgress(() => infinite.reload())

  // 点击媒体（图片与视频统一）打开文件详情面板（FR-34）
  const handleOpen = useCallback((f: MediaFile) => {
    const i = infinite.items.findIndex(x => x.id === f.id)
    if (i >= 0) setDetailIndex(i)
  }, [infinite.items])

  // 切换收藏（FR-41）：调用接口后刷新列表
  const handleToggleFavorite = useCallback(async (f: MediaFile) => {
    try {
      await libApi.setMediaFavorite(f.id, !f.favorite)
      infinite.reload()
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '更新收藏失败') })
    }
  }, [infinite])

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

      {/* 搜索（FR-35 表达式：ext: / type: / size: / 裸词） */}
      <TextInput
        placeholder="搜索：文件名，或 ext:jpg type:image size:>10mb"
        leftSection={<IconSearch size={14} />}
        value={infinite.searchInput}
        onChange={(e) => infinite.setSearchInput(e.target.value)}
        size="sm"
      />

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
      />

      <MediaDetailPanel
        files={infinite.items}
        initialIndex={detailIndex}
        onClose={() => setDetailIndex(null)}
        customImageExtensions={exts}
      />
    </Stack>
  )
}
