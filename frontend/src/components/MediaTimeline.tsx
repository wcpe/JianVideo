import { SimpleGrid, Card, Text, TextInput, ActionIcon, Group, Pagination, Box, Badge, Skeleton, Alert } from '@mantine/core'
import { IconSearch, IconTrash, IconPencil, IconFolder, IconAlertCircle } from '@tabler/icons-react'
import { formatSize, formatDuration } from '@/utils/format'
import { isImageFile } from '@/utils/media'
import MediaThumbnail from '@/components/MediaThumbnail'
import type { MediaFile } from '@/types'

interface MediaTimelineProps {
  mediaFiles: MediaFile[]
  total: number
  page: number
  searchInput: string
  loading: boolean
  error: string | null
  totalPages: number
  customImageExtensions: Record<number, string[]>
  onSearchChange: (value: string) => void
  onPageChange: (page: number) => void
  onErrorClose: () => void
  onOpenFile: (file: MediaFile) => void
  onDeleteFile?: (file: MediaFile) => void
  onRenameFile?: (file: MediaFile) => void
}

/** 时间轴视图 UI 组件：搜索框 + 媒体卡片网格 + 分页 */
export default function MediaTimeline({
  mediaFiles,
  total,
  page,
  searchInput,
  loading,
  error,
  totalPages,
  customImageExtensions,
  onSearchChange,
  onPageChange,
  onErrorClose,
  onOpenFile,
  onDeleteFile,
  onRenameFile,
}: MediaTimelineProps) {
  return (
    <>
      {/* 搜索 */}
      <TextInput
        placeholder="搜索文件名..."
        leftSection={<IconSearch size={14} />}
        value={searchInput}
        onChange={(e) => onSearchChange(e.target.value)}
        size="sm"
        mb="sm"
      />

      {error && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" withCloseButton onClose={onErrorClose} mb="sm">
          {error}
        </Alert>
      )}

      {/* 媒体列表 */}
      {loading ? (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} height={100} radius="md" />
          ))}
        </SimpleGrid>
      ) : mediaFiles.length === 0 ? (
        <Box py="xl" ta="center">
          <Box mb="sm" style={{ textAlign: 'center' }}>
            <IconFolder size={48} color="var(--mantine-color-dark-3)" />
          </Box>
          <Text c="dimmed">{searchInput ? '未找到匹配的文件' : '暂无媒体文件'}</Text>
          <Text c="dimmed" size="sm">添加目录后点击"扫描"按钮索引视频</Text>
        </Box>
      ) : (
        <>
          <Text size="sm" c="dimmed">共 {total} 个文件</Text>
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
            {mediaFiles.map((file) => (
              <Card
                key={file.id}
                withBorder p="md" radius="md" bg="dark.7"
                style={{ cursor: 'pointer' }}
                onClick={() => onOpenFile(file)}
                className="hover-card"
              >
                {isImageFile(file, customImageExtensions) && (
                  <Box mb="xs">
                    <MediaThumbnail mediaID={file.id} fileName={file.file_name} />
                  </Box>
                )}
                <Text fw={500} truncate mb="xs">{file.file_name}</Text>
                <Group gap="xs" wrap="nowrap">
                  <Badge size="xs" variant="light" color="purple">{formatSize(file.file_size)}</Badge>
                  <Badge size="xs" variant="light" color="blue">{file.format.toUpperCase()}</Badge>
                  {!isImageFile(file, customImageExtensions) && <Badge size="xs" variant="light" color="gray">{formatDuration(file.duration)}</Badge>}
                  {file.width > 0 && file.height > 0 && (
                    <Badge size="xs" variant="light" color="gray">{file.width}x{file.height}</Badge>
                  )}
                </Group>
                {(onRenameFile || onDeleteFile) && (
                  <Group justify="flex-end" mt="xs">
                    {onRenameFile && (
                      <ActionIcon
                        size="sm" variant="subtle" color="gray"
                        aria-label="重命名"
                        onClick={(e) => { e.stopPropagation(); onRenameFile(file) }}
                      >
                        <IconPencil size={14} />
                      </ActionIcon>
                    )}
                    {onDeleteFile && (
                      <ActionIcon
                        size="sm" variant="subtle" color="red"
                        aria-label="删除"
                        onClick={(e) => { e.stopPropagation(); onDeleteFile(file) }}
                      >
                        <IconTrash size={14} />
                      </ActionIcon>
                    )}
                  </Group>
                )}
              </Card>
            ))}
          </SimpleGrid>
        </>
      )}

      {totalPages > 1 && (
        <Group justify="center" mt="md">
          <Pagination total={totalPages} value={page} onChange={onPageChange} size="sm" color="purple" />
        </Group>
      )}
    </>
  )
}
