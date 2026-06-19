import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Button, Text, Group, Paper, Badge, Skeleton, Alert, Stack, Title } from '@mantine/core'
import { IconArrowLeft, IconAlertCircle } from '@tabler/icons-react'
import VideoPlayer from '@/components/VideoPlayer'
import * as libApi from '@/api/library'
import type { MediaFile } from '@/types'

/** 视频播放页 — Mantine + VideoPlayer */
export default function PlayPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [media, setMedia] = useState<MediaFile | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    const mediaId = parseInt(id, 10)
    if (isNaN(mediaId) || mediaId <= 0) {
      setError('无效的媒体 ID')
      setLoading(false)
      return
    }

    libApi.getMediaFile(mediaId)
      .then((data) => setMedia(data))
      .catch(() => setError('媒体文件不存在'))
      .finally(() => setLoading(false))
  }, [id])

  if (loading) {
    return <Skeleton height={400} radius="md" />
  }

  if (error || !media) {
    return (
      <Alert icon={<IconAlertCircle size={16} />} color="red" withCloseButton onClose={() => navigate(-1)}>
        {error || '媒体文件不存在'}
      </Alert>
    )
  }

  const hlsUrl = `/api/play/hls/${media.id}/index.m3u8`

  return (
    <Stack gap="md">
      {/* 返回 + 标题 */}
      <Group gap="sm">
        <Button
          variant="subtle"
          color="gray"
          size="sm"
          leftSection={<IconArrowLeft size={14} />}
          onClick={() => navigate(-1)}
        >
          返回
        </Button>
        <Title order={3}>{media.file_name}</Title>
      </Group>

      {/* 播放器 */}
      <VideoPlayer url={hlsUrl} autoPlay />

      {/* 媒体信息 */}
      <Paper withBorder p="md" radius="md" bg="dark.7">
        <Text fw={600} size="sm" mb="sm">媒体信息</Text>
        <Group gap="md" wrap="wrap">
          <div>
            <Text size="xs" c="dimmed">路径</Text>
            <Text size="sm" truncate maw={400}>{media.file_path}</Text>
          </div>
          <div>
            <Text size="xs" c="dimmed">格式</Text>
            <Badge size="sm" variant="light" color="blue">{media.format.toUpperCase()}</Badge>
          </div>
          <div>
            <Text size="xs" c="dimmed">编码</Text>
            <Text size="sm">{media.video_codec || '-'} / {media.audio_codec || '-'}</Text>
          </div>
          <div>
            <Text size="xs" c="dimmed">分辨率</Text>
            <Text size="sm">{media.width > 0 ? `${media.width}x${media.height}` : '-'}</Text>
          </div>
        </Group>
      </Paper>
    </Stack>
  )
}
