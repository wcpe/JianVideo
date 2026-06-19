import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Button, Text, Group, Paper, Badge, Skeleton, Alert, Stack, Title, Menu } from '@mantine/core'
import { IconArrowLeft, IconAlertCircle, IconSubtitles } from '@tabler/icons-react'
import VideoPlayer from '@/components/VideoPlayer'
import { parseWebVTT } from '@/components/VideoPlayer'
import * as libApi from '@/api/library'
import * as subtitleApi from '@/api/subtitle'
import type { MediaFile, SubtitleTrack } from '@/types'

/** 一条解析后的字幕条目 */
interface SubtitleEntry {
  start: number
  end: number
  text: string
}

/** 视频播放页 — Mantine + VideoPlayer */
export default function PlayPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [media, setMedia] = useState<MediaFile | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // 字幕状态
  const [subtitleTracks, setSubtitleTracks] = useState<SubtitleTrack[]>([])
  const [selectedTrack, setSelectedTrack] = useState<number | null>(null)
  const [subtitleEntries, setSubtitleEntries] = useState<SubtitleEntry[]>([])
  const [subtitleVisible, setSubtitleVisible] = useState(false)
  const [subtitleMenuOpened, setSubtitleMenuOpened] = useState(false)

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

    // 加载字幕轨道列表
    subtitleApi.getSubtitles(mediaId)
      .then((tracks) => setSubtitleTracks(tracks))
      .catch(() => setSubtitleTracks([]))
  }, [id])

  // 选择字幕轨道
  const selectTrack = async (trackIndex: number) => {
    if (!media) return
    setSelectedTrack(trackIndex)
    setSubtitleVisible(true)
    setSubtitleMenuOpened(false)

    try {
      const vttContent = await subtitleApi.getSubtitleContent(media.id, trackIndex)
      setSubtitleEntries(parseWebVTT(vttContent))
    } catch {
      setSubtitleEntries([])
    }
  }

  // 关闭字幕
  const disableSubtitles = () => {
    setSelectedTrack(null)
    setSubtitleVisible(false)
    setSubtitleEntries([])
    setSubtitleMenuOpened(false)
  }

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

  const hlsUrl = `/api/play/hls/${media.id}/master.m3u8`

  return (
    <Stack gap="md">
      {/* 返回 + 标题 + 字幕选择 */}
      <Group gap="sm" justify="space-between">
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

        {/* 字幕选择菜单 */}
        {subtitleTracks.length > 0 && (
          <Menu opened={subtitleMenuOpened} onChange={setSubtitleMenuOpened}>
            <Menu.Target>
              <Button
                variant="subtle"
                color="gray"
                size="sm"
                leftSection={<IconSubtitles size={14} />}
              >
                {selectedTrack !== null ? subtitleTracks[selectedTrack]?.file_name : '字幕'}
              </Button>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Item onClick={disableSubtitles}>关闭字幕</Menu.Item>
              {subtitleTracks.map((track) => (
                <Menu.Item
                  key={track.index}
                  onClick={() => selectTrack(track.index)}
                >
                  {track.file_name} ({track.format.toUpperCase()})
                </Menu.Item>
              ))}
            </Menu.Dropdown>
          </Menu>
        )}
      </Group>

      {/* 播放器 */}
      <VideoPlayer
        url={hlsUrl}
        autoPlay
        subtitleEntries={subtitleEntries}
        subtitleVisible={subtitleVisible}
      />

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
