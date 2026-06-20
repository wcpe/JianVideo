import { SimpleGrid, Card, Text, Group, Box, Badge, Skeleton, Alert, Stack } from '@mantine/core'
import { IconFolder, IconAlertCircle } from '@tabler/icons-react'
import { formatSize, formatDuration } from '@/utils/format'
import { isImageFile } from '@/utils/media'
import { groupMediaByDate } from '@/utils/timeline'
import MediaThumbnail from '@/components/MediaThumbnail'
import type { MediaFile } from '@/types'

interface TimelineViewProps {
  mediaFiles: MediaFile[]
  loading: boolean
  error: string | null
  customImageExtensions: Record<number, string[]>
  onErrorClose: () => void
  onOpenFile: (file: MediaFile) => void
}

/** 把 YYYY-MM-DD 拆成年份与月-日两段，便于竖向日期轴展示 */
function splitDate(date: string): { year: string; monthDay: string } {
  // 非法/未知日期整段作为月日展示，年份留空
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return { year: '', monthDay: date }
  return { year: date.slice(0, 4), monthDay: date.slice(5) }
}

/** 时间轴视图：左侧竖向日期轴 + 右侧该日期下的媒体缩略图网格 */
export default function TimelineView({
  mediaFiles,
  loading,
  error,
  customImageExtensions,
  onErrorClose,
  onOpenFile,
}: TimelineViewProps) {
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

  const groups = groupMediaByDate(mediaFiles)

  return (
    <Stack gap="xl">
      {groups.map((group) => {
        const { year, monthDay } = splitDate(group.date)
        return (
          <Group key={group.date} align="flex-start" wrap="nowrap" gap="md">
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
      })}
    </Stack>
  )
}
