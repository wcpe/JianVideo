import { useRef, useEffect, useState, useMemo } from 'react'
import { SimpleGrid, Card, Text, Group, Box, Badge, Skeleton, Alert, Stack, Loader, Center, ActionIcon, Checkbox } from '@mantine/core'
import { IconFolder, IconAlertCircle, IconStar, IconStarFilled } from '@tabler/icons-react'
import { useWindowVirtualizer } from '@tanstack/react-virtual'
import { formatSize, formatDuration } from '@/utils/format'
import { isImageFile, mediaDisplayName } from '@/utils/media'
import { groupMediaByDate } from '@/utils/timeline'
import MediaThumbnail from '@/components/MediaThumbnail'
import TimelineScrubber from '@/components/TimelineScrubber'
import MediaContextMenu, { type ContextMenuState } from '@/components/MediaContextMenu'
import { useMultiSelect, type ClickModifiers } from '@/hooks/useMultiSelect'
import type { MediaFile } from '@/types'
import type { DateGroup, TimelineGranularity } from '@/utils/timeline'

/**
 * 选择上下文（FR-69）：把多选状态与卡片交互回调下传给日期组渲染。
 * flatIndexOf 把 media id 映射到「全部已加载项展平后」的有序下标，供 Shift 区间等手势使用。
 */
interface SelectionContext {
  enabled: boolean
  checkboxMode: boolean
  isSelected: (id: number) => boolean
  flatIndexOf: (id: number) => number
  onCardClick: (index: number, mods: ClickModifiers) => void
  onCardContextMenu: (id: number, e: React.MouseEvent) => void
}

interface TimelineViewProps {
  mediaFiles: MediaFile[]
  loading: boolean
  error: string | null
  customImageExtensions: Record<number, string[]>
  onErrorClose: () => void
  onOpenFile: (file: MediaFile) => void
  // 切换收藏（FR-41）：传入时媒体卡片展示标星按钮
  onToggleFavorite?: (file: MediaFile) => void
  // 滚动到底部时触发加载更多
  onLoadMore?: () => void
  // 是否还有更多数据可加载
  hasMore?: boolean
  // 是否正在加载下一页（用于底部提示）
  loadingMore?: boolean
  // 分组粒度（FR-32 缩放）：日 / 月 / 年，默认日
  granularity?: TimelineGranularity
  // 多选与批量删除（FR-69），均可选；传入任一即启用选择手势与右键菜单
  onSelectionChange?: (ids: number[]) => void
  onBatchDelete?: (ids: number[]) => void
  onDeleteOne?: (file: MediaFile) => void
}

/** 单个日期组的预估高度（用于虚拟化初始测量，会被实际测量覆盖） */
const GROUP_ESTIMATE_SIZE = 320

/** 把分组键拆成年份与月-日两段，便于竖向日期轴展示（支持 年/年-月/年-月-日 三种粒度，FR-32） */
function splitDate(date: string): { year: string; monthDay: string } {
  if (/^\d{4}$/.test(date)) return { year: date, monthDay: '' } // 年粒度：仅年
  if (/^\d{4}-\d{2}$/.test(date)) return { year: date.slice(0, 4), monthDay: date.slice(5) } // 年-月
  if (/^\d{4}-\d{2}-\d{2}$/.test(date)) return { year: date.slice(0, 4), monthDay: date.slice(5) } // 年-月-日
  // 非法/未知日期整段作为月日展示，年份留空
  return { year: '', monthDay: date }
}

/** 渲染单个日期组：左侧竖向日期轴 + 右侧媒体缩略图网格 */
function DateGroupRow({
  group,
  customImageExtensions,
  onOpenFile,
  onToggleFavorite,
  selection,
}: {
  group: DateGroup
  customImageExtensions: Record<number, string[]>
  onOpenFile: (file: MediaFile) => void
  onToggleFavorite?: (file: MediaFile) => void
  selection: SelectionContext
}) {
  const { year, monthDay } = splitDate(group.date)
  // 主标签：日/月粒度为月日、年粒度为年；次标签：仅日/月粒度在上方显示年
  const primary = monthDay || year
  const secondary = monthDay ? year : ''
  return (
    <Group align="flex-start" wrap="nowrap" gap="md" pb="xl">
      {/* 左侧竖向日期轴：圆点 + 竖线 + 年/月日 */}
      <Box style={{ width: 80, flexShrink: 0, position: 'relative' }}>
        <Group gap={8} wrap="nowrap" align="center" mb={4}>
          <Box style={{ width: 10, height: 10, borderRadius: '50%', background: 'var(--mantine-color-purple-5)', flexShrink: 0 }} />
          {secondary && <Text size="xs" c="dimmed">{secondary}</Text>}
        </Group>
        <Text fw={700} size="lg" pl={18}>{primary}</Text>
        {/* 竖线营造时间线视觉 */}
        <Box style={{ position: 'absolute', left: 4, top: 14, bottom: -24, width: 2, background: 'var(--mantine-color-default-border)' }} />
      </Box>

      {/* 右侧媒体卡片网格 */}
      <Box style={{ flex: 1, minWidth: 0 }}>
        <SimpleGrid cols={{ base: 2, sm: 3, lg: 4 }}>
          {group.files.map((file) => {
            const isImage = isImageFile(file, customImageExtensions)
            const selected = selection.enabled && selection.isSelected(file.id)
            // 时间轴沿用「普通单击=打开」的画廊式交互；多选仅在带修饰键（Ctrl/Cmd/Shift）或复选框模式下触发。
            // 这样既不回归既有点击打开预览/播放流程，又叠加桌面级多选手势。
            const handleClick = (e: React.MouseEvent) => {
              const isSelectGesture = e.ctrlKey || e.metaKey || e.shiftKey || selection.checkboxMode
              if (selection.enabled && isSelectGesture) {
                selection.onCardClick(selection.flatIndexOf(file.id), e)
              } else {
                onOpenFile(file)
              }
            }
            return (
              <Card
                key={file.id}
                withBorder p="sm" radius="md"
                bg={selected ? 'var(--mantine-color-purple-light)' : 'var(--mantine-color-default)'}
                style={{ cursor: 'pointer', borderColor: selected ? 'var(--mantine-color-purple-5)' : undefined }}
                onClick={handleClick}
                onContextMenu={(e) => selection.onCardContextMenu(file.id, e)}
                className="hover-card"
                data-selected={selected || undefined}
              >
                {/* 视频与图片都展示缩略图，右上角叠加标星按钮（FR-41） */}
                <Box mb="xs" style={{ position: 'relative' }}>
                  <MediaThumbnail mediaID={file.id} fileName={file.file_name} />
                  {selection.enabled && selection.checkboxMode && (
                    <Checkbox
                      size="xs" checked={selected} readOnly tabIndex={-1}
                      aria-label={`选择 ${mediaDisplayName(file)}`}
                      style={{ position: 'absolute', top: 6, left: 6, zIndex: 1 }}
                    />
                  )}
                  {onToggleFavorite && (
                    <ActionIcon
                      variant="filled"
                      color={file.favorite ? 'yellow' : 'dark'}
                      size="sm"
                      aria-label={file.favorite ? '取消收藏' : '收藏'}
                      title={file.favorite ? '取消收藏' : '收藏'}
                      style={{ position: 'absolute', top: 6, right: 6, opacity: 0.9 }}
                      onClick={(e) => {
                        // 阻止冒泡，避免触发卡片打开
                        e.stopPropagation()
                        onToggleFavorite(file)
                      }}
                    >
                      {file.favorite ? <IconStarFilled size={14} /> : <IconStar size={14} />}
                    </ActionIcon>
                  )}
                </Box>
                <Text fw={500} size="sm" truncate mb="xs" title={mediaDisplayName(file)}>{mediaDisplayName(file)}</Text>
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
  onToggleFavorite,
  onLoadMore,
  hasMore,
  loadingMore,
  granularity = 'day',
  onSelectionChange,
  onBatchDelete,
  onDeleteOne,
}: TimelineViewProps) {
  // 列表容器 ref，用于计算窗口虚拟化所需的 scrollMargin（列表相对文档顶部的偏移）
  const listRef = useRef<HTMLDivElement>(null)
  const [scrollMargin, setScrollMargin] = useState(0)

  // 分组结果记忆化：既稳定下方 useMemo 的依赖，又避免每次渲染重复分组
  const groups = useMemo(
    () => (error || loading ? [] : groupMediaByDate(mediaFiles, granularity)),
    [error, loading, mediaFiles, granularity],
  )

  // 父组件关心选择（提供任一选择相关回调）时启用选择手势与右键菜单
  const selectionEnabled = !!(onSelectionChange || onBatchDelete || onDeleteOne)
  // 全部已加载项按分组渲染顺序展平为有序 id 列表——Ctrl+A / Shift 区间均以此为范围（虚拟列表边界：
  // 已加载多少就能选多少，未滚动触发 loadMore 的项不在范围内）。flatIndexOf 供卡片把 id 反查为下标。
  const flatIds = useMemo(() => groups.flatMap((g) => g.files.map((f) => f.id)), [groups])
  const flatIndex = useMemo(() => {
    const map = new Map<number, number>()
    flatIds.forEach((id, i) => map.set(id, i))
    return map
  }, [flatIds])
  const select = useMultiSelect(flatIds)
  const { selectedIds } = select
  const [menu, setMenu] = useState<ContextMenuState | null>(null)

  // 选中集变化上抛父组件（升序）
  useEffect(() => {
    if (selectionEnabled) onSelectionChange?.(Array.from(selectedIds).sort((a, b) => a - b))
    // 仅在选中集变化时上抛
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIds])

  // 右键媒体卡片：阻止系统菜单，记录光标位置与右键项 id（不改动选择集）
  function handleCardContextMenu(id: number, e: React.MouseEvent) {
    if (!selectionEnabled) return
    e.preventDefault()
    setMenu({ x: e.clientX, y: e.clientY, targetId: id })
  }

  // 「删除选中」：有选中按选中批量删；无选中删右键项
  function handleMenuDelete(targetId: number) {
    setMenu(null)
    if (selectedIds.size > 0) {
      onBatchDelete?.(Array.from(selectedIds).sort((a, b) => a - b))
    } else {
      const f = mediaFiles.find((x) => x.id === targetId)
      if (f) onDeleteOne?.(f)
    }
  }

  const selection: SelectionContext = {
    enabled: selectionEnabled,
    checkboxMode: select.checkboxMode,
    isSelected: select.isSelected,
    flatIndexOf: (id) => flatIndex.get(id) ?? -1,
    onCardClick: select.handleItemClick,
    onCardContextMenu: handleCardContextMenu,
  }

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

  // 滚动加载更多：底部哨兵进入视口时触发 onLoadMore。
  // 用 IntersectionObserver 而非依赖虚拟化 lastIndex 变化——group 级虚拟化下，已加载日期组
  // 全部渲染后 lastIndex 不再变化，会导致 loadMore 不再触发（无限滚动失效）。
  const sentinelRef = useRef<HTMLDivElement>(null)
  const loadMoreRef = useRef<() => void>(() => {})
  useEffect(() => {
    loadMoreRef.current = () => { if (hasMore && !loadingMore) onLoadMore?.() }
  }, [hasMore, loadingMore, onLoadMore])
  useEffect(() => {
    const el = sentinelRef.current
    // 无布局/无 IntersectionObserver 的环境（如测试 jsdom）降级为不挂载，避免崩溃
    if (!el || typeof IntersectionObserver === 'undefined') return
    const observer = new IntersectionObserver((entries) => {
      if (entries[0]?.isIntersecting) loadMoreRef.current()
    }, { rootMargin: '400px' })
    observer.observe(el)
    return () => observer.disconnect()
    // 列表在 loading/空 状态下不渲染哨兵，故依赖这些状态变化以在列表出现后重新挂载观察
  }, [loading, error, mediaFiles.length])

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
          <IconFolder size={48} color="var(--mantine-color-dimmed)" />
        </Box>
        <Text c="dimmed">暂无媒体文件</Text>
      </Box>
    )
  }

  return (
    <>
      {/* 虚拟化容器：高度撑满全部日期组总高，内部按虚拟项绝对定位 */}
      <Box ref={listRef} style={{ position: 'relative', height: virtualizer.getTotalSize() }}>
        {/* 可拖动时间 scrubber（FR-68）：拖动浮层预览、松手滚动跳转到目标分组 */}
        <TimelineScrubber
          groups={groups}
          onSeek={(index) => virtualizer.scrollToIndex(index, { align: 'start' })}
        />
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
                onToggleFavorite={onToggleFavorite}
                selection={selection}
              />
            </Box>
          )
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

      {/* 右键常用菜单（FR-69）：父组件关心选择时挂载 */}
      {selectionEnabled && (
        <MediaContextMenu
          state={menu}
          onClose={() => setMenu(null)}
          selectedCount={selectedIds.size}
          checkboxMode={select.checkboxMode}
          onDelete={handleMenuDelete}
          onSelectAll={() => { select.selectAll(); setMenu(null) }}
          onInvert={() => { select.invertSelection(); setMenu(null) }}
          onToggleCheckboxMode={() => { select.setCheckboxMode(!select.checkboxMode); setMenu(null) }}
        />
      )}
    </>
  )
}
