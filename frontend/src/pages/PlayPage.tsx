import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Button, Text, Group, Paper, Badge, Skeleton, Alert, Stack, Title, Menu } from '@mantine/core'
import { IconArrowLeft, IconAlertCircle, IconSubtitles } from '@tabler/icons-react'
import VideoPlayer from '@/components/VideoPlayer'
import { parseWebVTT } from '@/utils/subtitle'
import * as libApi from '@/api/library'
import * as subtitleApi from '@/api/subtitle'
import type { MediaFile, SubtitleTrack, SubtitleEntry } from '@/types'

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

  // 播放器 URL / ABR 模式：探测 master.m3u8 是否可用；不可用时降级到 /api/play/:id/stream
  const [playerUrl, setPlayerUrl] = useState<string | null>(null)
  const [playerIsABR, setPlayerIsABR] = useState<boolean | null>(null)

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

    // 解析为绝对 URL，避免 mpegts.js 在 Web Worker 中 fetch 相对 URL 失败。
    const toAbsolute = (path: string) => new URL(path, window.location.href).toString()
    const hlsUrl = toAbsolute(`/api/play/hls/${mediaId}/master`)
    const streamUrl = toAbsolute(`/api/play/${mediaId}/stream`)
    fetch(hlsUrl, { method: 'GET' }).then((resp) => {
      // master.m3u8 内容若为非 m3u8 文本（如 404 的 JSON 错误体），判定为不可用
      if (resp.ok && resp.headers.get('content-type')?.includes('mpegurl')) {
        setPlayerUrl(hlsUrl)
        setPlayerIsABR(true)
      } else {
        setPlayerUrl(streamUrl)
        setPlayerIsABR(false)
      }
    }).catch(() => {
      setPlayerUrl(streamUrl)
      setPlayerIsABR(false)
    })
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

      {/* 播放器：探测到 HLS 不可用时降级到 /api/play/:id/stream（非 ABR，浏览器原生 video） */}
      {playerUrl && playerIsABR !== null && (
        <VideoPlayer
          url={playerUrl}
          autoPlay
          subtitleEntries={subtitleEntries}
          subtitleVisible={subtitleVisible}
          isABR={playerIsABR}
          streamType={playerIsABR ? 'mpegts' : 'mp4'}
        />
      )}

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
