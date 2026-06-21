import { useRef, useEffect, useState } from 'react'
import { SimpleGrid, Card, Text, Group, Box, Skeleton, Alert, Stack, Badge, ActionIcon } from '@mantine/core'
import { IconFolder, IconTrash, IconAlertCircle } from '@tabler/icons-react'
import { useWindowVirtualizer } from '@tanstack/react-virtual'
import { formatSize, formatDuration } from '@/utils/format'
import { isImageFile, mediaDisplayName } from '@/utils/media'
import DirectoryBreadcrumb from '@/components/DirectoryBreadcrumb'
import MediaThumbnail from '@/components/MediaThumbnail'
import type { MediaFile, BreadcrumbItem, DirInfo } from '@/types'

interface DirectoryBrowserProps {
  breadcrumbs: BreadcrumbItem[]
  directories: DirInfo[]
  files: MediaFile[]
  loading: boolean
  error: string | null
  customImageExtensions: Record<number, string[]>
  onEnterDir: (path: string) => void
  onBreadcrumbNavigate: (path: string) => void
  onErrorClose: () => void
  onOpenFile: (file: MediaFile) => void
  onDeleteFile?: (file: MediaFile) => void
}

/** 单个条目的预估高度（用于虚拟化初始测量，会被实际测量覆盖） */
const ENTRY_ESTIMATE_SIZE = 120

/** 虚拟列表的有序条目：先目录后文件 */
type DirEntry = { kind: 'dir'; dir: DirInfo }
type FileEntry = { kind: 'file'; file: MediaFile }
type Entry = DirEntry | FileEntry

/** 目录卡片：点击进入子目录 */
function DirectoryCard({ dir, onEnterDir }: { dir: DirInfo; onEnterDir: (path: string) => void }) {
  return (
    <Card
      withBorder p="sm" radius="sm" bg="dark.7"
      style={{ cursor: 'pointer' }}
      onClick={() => onEnterDir(dir.path)}
      className="hover-card"
    >
      <Group gap="xs">
        <IconFolder size={16} color="var(--mantine-color-purple-4)" />
        <Text size="sm">{dir.name}</Text>
      </Group>
    </Card>
  )
}

/** 文件卡片：点击打开，图片显示缩略图，可选删除 */
function FileCard({
  file,
  customImageExtensions,
  onOpenFile,
  onDeleteFile,
}: {
  file: MediaFile
  customImageExtensions: Record<number, string[]>
  onOpenFile: (file: MediaFile) => void
  onDeleteFile?: (file: MediaFile) => void
}) {
  const isImage = isImageFile(file, customImageExtensions)
  return (
    <Card
      withBorder p="md" radius="md" bg="dark.7"
      style={{ cursor: 'pointer' }}
      onClick={() => onOpenFile(file)}
      className="hover-card"
    >
      {isImage && (
        <Box mb="xs">
          <MediaThumbnail mediaID={file.id} fileName={file.file_name} />
        </Box>
      )}
      <Text fw={500} truncate mb="xs" title={mediaDisplayName(file)}>{mediaDisplayName(file)}</Text>
      <Group gap="xs" wrap="nowrap">
        <Badge size="xs" variant="light" color="purple">{formatSize(file.file_size)}</Badge>
        <Badge size="xs" variant="light" color="blue">{file.format.toUpperCase()}</Badge>
        {!isImage && <Badge size="xs" variant="light" color="gray">{formatDuration(file.duration)}</Badge>}
        {file.width > 0 && file.height > 0 && (
          <Badge size="xs" variant="light" color="gray">{file.width}x{file.height}</Badge>
        )}
      </Group>
      {onDeleteFile && (
        <Group justify="flex-end" mt="xs">
          <ActionIcon
            size="sm" variant="subtle" color="red"
            aria-label="删除"
            onClick={(e) => { e.stopPropagation(); onDeleteFile(file) }}
          >
            <IconTrash size={14} />
          </ActionIcon>
        </Group>
      )}
    </Card>
  )
}

/**
 * 目录浏览 UI 组件：面包屑导航 + 窗口虚拟化的条目列表（先目录后文件）。
 * 只渲染可见区 + overscan，目录条目很多时也流畅。
 */
export default function DirectoryBrowser({
  breadcrumbs,
  directories,
  files,
  loading,
  error,
  customImageExtensions,
  onEnterDir,
  onBreadcrumbNavigate,
  onErrorClose,
  onOpenFile,
  onDeleteFile,
}: DirectoryBrowserProps) {
  // 列表容器 ref，用于计算窗口虚拟化所需的 scrollMargin（列表相对文档顶部的偏移）
  const listRef = useRef<HTMLDivElement>(null)
  const [scrollMargin, setScrollMargin] = useState(0)

  // 合并为有序条目：先目录后文件，保持原顺序
  const entries: Entry[] = loading
    ? []
    : [
        ...directories.map((dir): Entry => ({ kind: 'dir', dir })),
        ...files.map((file): Entry => ({ kind: 'file', file })),
      ]

  // 列表挂载/数据变化后测量容器距文档顶部的偏移作为 scrollMargin
  useEffect(() => {
    if (listRef.current) {
      setScrollMargin(listRef.current.getBoundingClientRect().top + window.scrollY)
    }
  }, [entries.length])

  const virtualizer = useWindowVirtualizer({
    count: entries.length,
    estimateSize: () => ENTRY_ESTIMATE_SIZE,
    overscan: 4,
    scrollMargin,
  })

  return (
    <>
      {error && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" withCloseButton onClose={onErrorClose} mb="sm">
          {error}
        </Alert>
      )}

      {/* 面包屑导航 */}
      {breadcrumbs.length > 0 && (
        <Box mb="sm">
          <DirectoryBreadcrumb items={breadcrumbs} onNavigate={onBreadcrumbNavigate} />
        </Box>
      )}

      {/* 目录浏览内容 */}
      {loading ? (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} height={60} radius="md" />
          ))}
        </SimpleGrid>
      ) : entries.length === 0 ? (
        // 空状态
        <Stack gap="xs">
          <Box py="xl" ta="center">
            <Box mb="sm" style={{ textAlign: 'center' }}>
              <IconFolder size={48} color="var(--mantine-color-dimmed)" />
            </Box>
            <Text c="dimmed">此目录暂无内容</Text>
          </Box>
        </Stack>
      ) : (
        // 虚拟化容器：高度撑满全部条目总高，内部按虚拟项绝对定位
        <Box ref={listRef} style={{ position: 'relative', height: virtualizer.getTotalSize() }}>
          {virtualizer.getVirtualItems().map((virtualItem) => {
            const entry = entries[virtualItem.index]
            const key = entry.kind === 'dir' ? `dir-${entry.dir.path}` : `file-${entry.file.id}`
            return (
              <Box
                key={key}
                data-index={virtualItem.index}
                ref={virtualizer.measureElement}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  // 条目之间留间距，等价于原 Stack gap="xs"
                  paddingBottom: 8,
                  // 减去 scrollMargin 修正窗口虚拟化的起始偏移
                  transform: `translateY(${virtualItem.start - virtualizer.options.scrollMargin}px)`,
                }}
              >
                {entry.kind === 'dir' ? (
                  <DirectoryCard dir={entry.dir} onEnterDir={onEnterDir} />
                ) : (
                  <FileCard
                    file={entry.file}
                    customImageExtensions={customImageExtensions}
                    onOpenFile={onOpenFile}
                    onDeleteFile={onDeleteFile}
                  />
                )}
              </Box>
            )
          })}
        </Box>
      )}
    </>
  )
}
