import { SimpleGrid, Card, Text, Group, Box, Skeleton, Alert, Stack, Badge, ActionIcon } from '@mantine/core'
import { IconFolder, IconTrash, IconAlertCircle } from '@tabler/icons-react'
import { formatSize, formatDuration } from '@/utils/format'
import { isImageFile } from '@/utils/media'
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
  onDeleteFile: (file: MediaFile) => void
}

/** 目录浏览 UI 组件：面包屑导航 + 子目录列表 + 文件列表 */
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
      ) : (
        <Stack gap="xs">
          {/* 子目录列表 */}
          {directories.map((dir) => (
            <Card
              key={dir.path}
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
          ))}

          {/* 当前目录下的文件列表 */}
          {files.map((file) => (
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
              <Group justify="flex-end" mt="xs">
                <ActionIcon
                  size="sm" variant="subtle" color="red"
                  onClick={(e) => { e.stopPropagation(); onDeleteFile(file) }}
                >
                  <IconTrash size={14} />
                </ActionIcon>
              </Group>
            </Card>
          ))}

          {/* 空状态 */}
          {directories.length === 0 && files.length === 0 && (
            <Box py="xl" ta="center">
              <Box mb="sm" style={{ textAlign: 'center' }}>
                <IconFolder size={48} color="var(--mantine-color-dark-3)" />
              </Box>
              <Text c="dimmed">此目录暂无内容</Text>
            </Box>
          )}
        </Stack>
      )}
    </>
  )
}
