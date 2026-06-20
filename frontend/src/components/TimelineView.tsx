import { useRef, useEffect, useState } from 'react'
import { SimpleGrid, Card, Text, Group, Box, Badge, Skeleton, Alert, Stack, Loader, Center } from '@mantine/core'
import { IconFolder, IconAlertCircle } from '@tabler/icons-react'
import { useWindowVirtualizer } from '@tanstack/react-virtual'
import { formatSize, formatDuration } from '@/utils/format'
import { isImageFile } from '@/utils/media'
import { groupMediaByDate } from '@/utils/timeline'
import MediaThumbnail from '@/components/MediaThumbnail'
import type { MediaFile } from '@/types'
import type { DateGroup } from '@/utils/timeline'

interface TimelineViewProps {
  mediaFiles: MediaFile[]
  loading: boolean
  error: string | null
  customImageExtensions: Record<number, string[]>
  onErrorClose: () => void
  onOpenFile: (file: MediaFile) => void
  // 滚动到底部时触发加载更多
  onLoadMore?: () => void
  // 是否还有更多数据可加载
  hasMore?: boolean
  // 是否正在加载下一页（用于底部提示）
  loadingMore?: boolean
}

/** 单个日期组的预估高度（用于虚拟化初始测量，会被实际测量覆盖） */
const GROUP_ESTIMATE_SIZE = 320

/** 把 YYYY-MM-DD 拆成年份与月-日两段，便于竖向日期轴展示 */
function splitDate(date: string): { year: string; monthDay: string } {
  // 非法/未知日期整段作为月日展示，年份留空
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return { year: '', monthDay: date }
  return { year: date.slice(0, 4), monthDay: date.slice(5) }
}

/** 渲染单个日期组：左侧竖向日期轴 + 右侧媒体缩略图网格 */
function DateGroupRow({
  group,
  customImageExtensions,
  onOpenFile,
}: {
  group: DateGroup
  customImageExtensions: Record<number, string[]>
  onOpenFile: (file: MediaFile) => void
}) {
  const { year, monthDay } = splitDate(group.date)
  return (
    <Group align="flex-start" wrap="nowrap" gap="md" pb="xl">
      {/* 左侧竖向日期轴：圆点 + 竖线 + 年/月日 */}
      <Box style={{ width: 80, flexShrink: 0, position: 'relative' }}>
        <Group gap={8} wrap="nowrap" align="center" mb={4}>
          <Box style={{ width: 10, height: 10, borderRadius: '50%', background: 'var(--mantine-color-purple-5)', flexShrink: 0 }} />
          {year && <Text size="xs" c="dimmed">{year}</Text>}
        </Group>
        <Text fw={700} size="lg" pl={18}>{monthDay}</Text>
        {/* 竖线营造时间线视觉 */}
        <Box style={{ position: 'absolute', left: 4, top: 14, bottom: -24, width: 2, background: 'var(--mantine-color-dark-4)' }} />
      </Box>

      {/* 右侧媒体卡片网格 */}
      <Box style={{ flex: 1, minWidth: 0 }}>
        <SimpleGrid cols={{ base: 2, sm: 3, lg: 4 }}>
          {group.files.map((file) => {
            const isImage = isImageFile(file, customImageExtensions)
            return (
              <Card
                key={file.id}
                withBorder p="sm" radius="md" bg="dark.7"
                style={{ cursor: 'pointer' }}
                onClick={() => onOpenFile(file)}
                className="hover-card"
              >
                {/* 视频与图片都展示缩略图 */}
                <Box mb="xs">
                  <MediaThumbnail mediaID={file.id} fileName={file.file_name} />
                </Box>
                <Text fw={500} size="sm" truncate mb="xs">{file.file_name}</Text>
                <Group gap="xs" wrap="nowrap">
                  <Badge size="xs" variant="light" color="blue">{file.format.toUpperCase()}</Badge>
                  <Badge size="xs" variant="light" color="purple">{formatSize(file.file_size)}</Badge>
                  {!isImage && <Badge size="xs" variant="light" color="gray">{formatDuration(file.duration)}</Badge>}
                </Group>
              </Card>
            )
          })}
        </SimpleGrid>
      </Box>
    </Group>
  )
}

/**
 * 时间轴视图：按日期分组后用窗口虚拟化渲染日期组列表。
 * 只渲染可见区 + overscan，千条数据也流畅；滚动到底部自动加载更多。
 */
export default function TimelineView({
  mediaFiles,
  loading,
  error,
  customImageExtensions,
  onErrorClose,
  onOpenFile,
  onLoadMore,
  hasMore,
  loadingMore,
}: TimelineViewProps) {
  // 列表容器 ref，用于计算窗口虚拟化所需的 scrollMargin（列表相对文档顶部的偏移）
  const listRef = useRef<HTMLDivElement>(null)
  const [scrollMargin, setScrollMargin] = useState(0)

  const groups = error || loading ? [] : groupMediaByDate(mediaFiles)

  // 列表挂载/数据变化后测量容器距文档顶部的偏移作为 scrollMargin
  useEffect(() => {
    if (listRef.current) {
      setScrollMargin(listRef.current.getBoundingClientRect().top + window.scrollY)
    }
  }, [groups.length])

  const virtualizer = useWindowVirtualizer({
    count: groups.length,
    estimateSize: () => GROUP_ESTIMATE_SIZE,
    overscan: 4,
    scrollMargin,
  })

  const virtualItems = virtualizer.getVirtualItems()

  // 当虚拟渲染抵达最后一个日期组且仍有更多数据时触发加载更多。
  // 放在 effect 中以避免渲染期调用 setState。
  const lastIndex = virtualItems.length > 0 ? virtualItems[virtualItems.length - 1].index : -1
  useEffect(() => {
    if (hasMore && !loadingMore && lastIndex >= groups.length - 1 && groups.length > 0) {
      onLoadMore?.()
    }
  }, [lastIndex, hasMore, loadingMore, groups.length, onLoadMore])

  if (error) {
    return (
      <Alert icon={<IconAlertCircle size={16} />} color="red" withCloseButton onClose={onErrorClose}>
        {error}
      </Alert>
    )
  }

  if (loading) {
    return (
      <Stack gap="md">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} height={140} radius="md" />
        ))}
      </Stack>
    )
  }

  if (mediaFiles.length === 0) {
    return (
      <Box py="xl" ta="center">
        <Box mb="sm" style={{ textAlign: 'center' }}>
          <IconFolder size={48} color="var(--mantine-color-dark-3)" />
        </Box>
        <Text c="dimmed">暂无媒体文件</Text>
      </Box>
    )
  }

  return (
    <>
      {/* 虚拟化容器：高度撑满全部日期组总高，内部按虚拟项绝对定位 */}
      <Box ref={listRef} style={{ position: 'relative', height: virtualizer.getTotalSize() }}>
        {virtualItems.map((virtualItem) => {
          const group = groups[virtualItem.index]
          return (
            <Box
              key={group.date}
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
              <DateGroupRow
                group={group}
                customImageExtensions={customImageExtensions}
                onOpenFile={onOpenFile}
              />
            </Box>
          )
        })}
      </Box>

      {/* 加载更多提示 */}
      {loadingMore && (
        <Center py="md">
          <Loader size="sm" color="purple" />
        </Center>
      )}
    </>
  )
}
