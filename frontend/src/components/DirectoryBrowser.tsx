import { useState, useMemo } from 'react'
import { SimpleGrid, Card, Text, Group, Box, Skeleton, Alert, Stack, Badge } from '@mantine/core'
import { IconFolder, IconAlertCircle } from '@tabler/icons-react'
import { formatSize, formatDuration } from '@/utils/format'
import { isImageFile, mediaDisplayName } from '@/utils/media'
import DirectoryBreadcrumb from '@/components/DirectoryBreadcrumb'
import MediaThumbnail from '@/components/MediaThumbnail'
import type { MediaFile, BreadcrumbItem, DirInfo } from '@/types'

/** 展示方式（FR-33）：列表详情 / 大-中-小图标 */
export type DisplayMode = 'list' | 'large' | 'medium' | 'small'
/** 排序方式（FR-33） */
export type DirSort = 'name' | 'size' | 'type' | 'time'

interface DirectoryBrowserProps {
  breadcrumbs: BreadcrumbItem[]
  directories: DirInfo[]
  files: MediaFile[]
  loading: boolean
  error: string | null
  customImageExtensions: Record<number, string[]>
  onEnterDir: (dir: DirInfo) => void
  onBreadcrumbNavigate: (path: string) => void
  onErrorClose: () => void
  /** 双击文件触发打开（FR-33）；参数为该文件在排序后 files 中的下标 */
  onOpenFile: (file: MediaFile, index: number) => void
  // 展示方式与排序（FR-33），缺省 list / name
  displayMode?: DisplayMode
  sort?: DirSort
}

// 各档位的网格列数（图标档）
const GRID_COLS: Record<Exclude<DisplayMode, 'list'>, { base: number; sm: number; lg: number }> = {
  large: { base: 2, sm: 3, lg: 4 },
  medium: { base: 3, sm: 5, lg: 6 },
  small: { base: 4, sm: 6, lg: 8 },
}

/** 按排序方式对文件排序（纯函数，不改输入）。 */
export function sortFiles(files: MediaFile[], sort: DirSort): MediaFile[] {
  const arr = [...files]
  switch (sort) {
    case 'size':
      return arr.sort((a, b) => a.file_size - b.file_size)
    case 'type':
      return arr.sort((a, b) => (a.format || '').localeCompare(b.format || '') || a.file_name.localeCompare(b.file_name))
    case 'time':
      return arr.sort((a, b) => (a.modified_at || '').localeCompare(b.modified_at || ''))
    default:
      return arr.sort((a, b) => mediaDisplayName(a).localeCompare(mediaDisplayName(b)))
  }
}

/**
 * 目录浏览 UI（FR-33 资源管理器视图）：面包屑 + 多展示档位 + 排序 + 单选/Shift 多选。
 * 目录恒在文件前、目录按名称排序；双击文件打开详情面板。
 */
export default function DirectoryBrowser({
  breadcrumbs, directories, files, loading, error, customImageExtensions,
  onEnterDir, onBreadcrumbNavigate, onErrorClose, onOpenFile,
  displayMode = 'list', sort = 'name',
}: DirectoryBrowserProps) {
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [anchorIndex, setAnchorIndex] = useState<number | null>(null)

  const sortedDirs = useMemo(() => [...directories].sort((a, b) => a.name.localeCompare(b.name)), [directories])
  const sortedFiles = useMemo(() => sortFiles(files, sort), [files, sort])

  // 文件单击选择：默认单选；Shift 区间选；Ctrl/Cmd 切换
  function handleFileClick(index: number, e: React.MouseEvent) {
    if (e.shiftKey && anchorIndex !== null) {
      const [lo, hi] = anchorIndex < index ? [anchorIndex, index] : [index, anchorIndex]
      const next = new Set<number>()
      for (let i = lo; i <= hi; i++) next.add(sortedFiles[i].id)
      setSelectedIds(next)
    } else if (e.ctrlKey || e.metaKey) {
      const next = new Set(selectedIds)
      const id = sortedFiles[index].id
      if (next.has(id)) next.delete(id)
      else next.add(id)
      setSelectedIds(next)
      setAnchorIndex(index)
    } else {
      setSelectedIds(new Set([sortedFiles[index].id]))
      setAnchorIndex(index)
    }
  }

  if (error) {
    return (
      <Alert icon={<IconAlertCircle size={16} />} color="red" withCloseButton onClose={onErrorClose} mb="sm">
        {error}
      </Alert>
    )
  }

  const isList = displayMode === 'list'
  const cols = isList ? undefined : GRID_COLS[displayMode]

  return (
    <>
      {breadcrumbs.length > 0 && (
        <Box mb="sm">
          <DirectoryBreadcrumb items={breadcrumbs} onNavigate={onBreadcrumbNavigate} />
        </Box>
      )}

      {loading ? (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} height={60} radius="md" />)}
        </SimpleGrid>
      ) : sortedDirs.length === 0 && sortedFiles.length === 0 ? (
        <Box py="xl" ta="center">
          <Box mb="sm" style={{ textAlign: 'center' }}>
            <IconFolder size={48} color="var(--mantine-color-dimmed)" />
          </Box>
          <Text c="dimmed">此目录暂无内容</Text>
        </Box>
      ) : isList ? (
        // 列表（详情行）
        <Stack gap={4}>
          {sortedDirs.map((dir) => (
            <Card key={`dir-${dir.path}`} withBorder p="xs" radius="sm" bg="var(--mantine-color-default)"
              style={{ cursor: 'pointer' }} className="hover-card" onClick={() => onEnterDir(dir)}>
              <Group gap="xs" wrap="nowrap">
                <IconFolder size={18} color="var(--mantine-color-purple-4)" />
                <Text size="sm">{dir.name}</Text>
              </Group>
            </Card>
          ))}
          {sortedFiles.map((file, i) => {
            const isImage = isImageFile(file, customImageExtensions)
            const selected = selectedIds.has(file.id)
            return (
              <Card key={`file-${file.id}`} withBorder p="xs" radius="sm"
                bg={selected ? 'var(--mantine-color-purple-light)' : 'var(--mantine-color-default)'}
                style={{ cursor: 'pointer', borderColor: selected ? 'var(--mantine-color-purple-5)' : undefined }}
                className="hover-card"
                onClick={(e) => handleFileClick(i, e)}
                onDoubleClick={() => onOpenFile(file, i)}
                data-selected={selected || undefined}
              >
                <Group gap="sm" wrap="nowrap" justify="space-between">
                  <Text size="sm" truncate style={{ flex: 1, minWidth: 0 }} title={mediaDisplayName(file)}>{mediaDisplayName(file)}</Text>
                  <Group gap="xs" wrap="nowrap" style={{ flexShrink: 0 }}>
                    <Badge size="xs" variant="light" color="blue">{file.format.toUpperCase()}</Badge>
                    <Badge size="xs" variant="light" color="purple">{formatSize(file.file_size)}</Badge>
                    {!isImage && <Badge size="xs" variant="light" color="gray">{formatDuration(file.duration)}</Badge>}
                    {file.width > 0 && file.height > 0 && <Badge size="xs" variant="light" color="gray">{file.width}x{file.height}</Badge>}
                  </Group>
                </Group>
              </Card>
            )
          })}
        </Stack>
      ) : (
        // 图标档（大/中/小）
        <SimpleGrid cols={cols}>
          {sortedDirs.map((dir) => (
            <Card key={`dir-${dir.path}`} withBorder p="sm" radius="sm" bg="var(--mantine-color-default)"
              style={{ cursor: 'pointer' }} className="hover-card" onClick={() => onEnterDir(dir)}>
              <Stack gap={4} align="center">
                <IconFolder size={displayMode === 'small' ? 24 : 40} color="var(--mantine-color-purple-4)" />
                <Text size="xs" truncate w="100%" ta="center">{dir.name}</Text>
              </Stack>
            </Card>
          ))}
          {sortedFiles.map((file, i) => {
            const selected = selectedIds.has(file.id)
            return (
              <Card key={`file-${file.id}`} withBorder p={displayMode === 'small' ? 4 : 'sm'} radius="md"
                bg={selected ? 'var(--mantine-color-purple-light)' : 'var(--mantine-color-default)'}
                style={{ cursor: 'pointer', borderColor: selected ? 'var(--mantine-color-purple-5)' : undefined }}
                className="hover-card"
                onClick={(e) => handleFileClick(i, e)}
                onDoubleClick={() => onOpenFile(file, i)}
                data-selected={selected || undefined}
              >
                <Box mb={4}>
                  <MediaThumbnail mediaID={file.id} fileName={file.file_name} />
                </Box>
                {displayMode !== 'small' && (
                  <Text size="xs" truncate title={mediaDisplayName(file)}>{mediaDisplayName(file)}</Text>
                )}
              </Card>
            )
          })}
        </SimpleGrid>
      )}
    </>
  )
}
