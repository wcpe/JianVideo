import { useState, useEffect, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Button, Text, Group, Paper, Badge, Skeleton, Alert, Stack, Title, Menu } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconArrowLeft, IconAlertCircle, IconSubtitles, IconPencil, IconFileText, IconDownload, IconShare, IconExternalLink, IconMovie } from '@tabler/icons-react'
import VideoPlayer from '@/components/VideoPlayer'
import NameEditModal from '@/components/NameEditModal'
import ShareDialog from '@/components/ShareDialog'
import ExternalPlayerDialog from '@/components/ExternalPlayerDialog'
import PregenDialog from '@/components/PregenDialog'
import { parseWebVTT } from '@/utils/subtitle'
import { mediaDisplayName } from '@/utils/media'
import { probeClientCapabilities } from '@/utils/codec-capability'
import * as libApi from '@/api/library'
import * as playApi from '@/api/play'
import * as subtitleApi from '@/api/subtitle'
import type { MediaFile, SubtitleTrack, SubtitleEntry, PlaybackDescriptor } from '@/types'

// 双模式改名弹窗类型：显示名（仅库内）/ 真实文件名（磁盘改名）
type NameEditKind = 'display' | 'real'

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
  // 编码协商描述符（FR-53）：协商出高级编码（fMP4）时交自适应播放器；
  // 协商出 h264 / 协商失败则为 null，沿用下方 master 探测的 H.264/TS 路径（不报错）。
  const [descriptor, setDescriptor] = useState<PlaybackDescriptor | null>(null)

  // 双模式改名（FR-30）：null 表示弹窗关闭
  const [nameEditKind, setNameEditKind] = useState<NameEditKind | null>(null)

  // 分享弹窗开关（FR-43）
  const [shareOpened, setShareOpened] = useState(false)
  // 外部播放器深链弹窗开关（FR-79）
  const [extPlayerOpened, setExtPlayerOpened] = useState(false)
  // 加入预生成队列弹窗开关（FR-77）
  const [pregenOpened, setPregenOpened] = useState(false)
  const [nameEditSaving, setNameEditSaving] = useState(false)

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

    // master 探测 → mpegts.js（ABR）或 stream（原生 mp4），既有 H.264/TS 路径不变。
    const probeMaster = () => {
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
    }

    // 端到端编码协商（FR-53）：探测客户端能力 → 请求协商 → 拿描述符。
    // 协商出高级编码（fMP4）→ 交自适应播放器；协商出 h264 / 协商失败 → 回退既有 master 探测（不报错）。
    playApi.negotiate(mediaId, probeClientCapabilities())
      .then((desc) => {
        if (desc.path === 'fmp4') {
          setDescriptor(desc)
          setPlayerUrl(desc.url)
          setPlayerIsABR(true)
        } else {
          probeMaster()
        }
      })
      .catch(() => probeMaster())
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

  // 续播与观看状态（FR-44）：上报播放位置、看完标记。失败仅静默忽略，不打断播放。
  const handlePositionReport = useCallback((position: number) => {
    if (!media) return
    void libApi.updateWatchPosition(media.id, position).catch(() => {})
  }, [media])

  const handleEnded = useCallback(() => {
    if (!media) return
    void libApi.markWatched(media.id).catch(() => {})
  }, [media])

  // 确认改名（FR-30）：display 仅改库内显示名，real 走磁盘改名
  const confirmNameEdit = async (value: string) => {
    if (!media || !nameEditKind) return
    setNameEditSaving(true)
    try {
      const updated = nameEditKind === 'display'
        ? await libApi.updateDisplayName(media.id, value)
        : await libApi.renameMediaFile(media.id, value)
      setMedia(updated)
      setNameEditKind(null)
      notifications.show({
        title: '修改成功',
        message: nameEditKind === 'display' ? '已更新显示名' : '已修改真实文件名',
        color: 'green',
        autoClose: 2500,
      })
    } catch (err) {
      notifications.show({
        title: '修改失败',
        message: err instanceof Error ? err.message : '请稍后重试',
        color: 'red',
        autoClose: 3000,
      })
    } finally {
      setNameEditSaving(false)
    }
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
          <Title order={3}>{mediaDisplayName(media)}</Title>
          {/* 双模式改名入口（FR-30）：均需二次确认 */}
          <Button
            variant="subtle"
            color="gray"
            size="sm"
            leftSection={<IconPencil size={14} />}
            onClick={() => setNameEditKind('display')}
          >
            改显示名
          </Button>
          <Button
            variant="subtle"
            color="gray"
            size="sm"
            leftSection={<IconFileText size={14} />}
            onClick={() => setNameEditKind('real')}
          >
            改文件名
          </Button>
          {/* 下载原文件（FR-42）：附件下载磁盘原始文件 */}
          <Button
            component="a"
            href={`/api/library/media/${media.id}/download`}
            download
            variant="subtle"
            color="gray"
            size="sm"
            leftSection={<IconDownload size={14} />}
          >
            下载
          </Button>
          {/* 分享链接（FR-43）：生成 token 化免登访问链接 */}
          <Button
            variant="subtle"
            color="gray"
            size="sm"
            leftSection={<IconShare size={14} />}
            onClick={() => setShareOpened(true)}
          >
            分享
          </Button>
          {/* 外部播放器深链（FR-79）：生成 VLC/IINA 可打开的免登网络串流地址 */}
          <Button
            variant="subtle"
            color="gray"
            size="sm"
            leftSection={<IconExternalLink size={14} />}
            onClick={() => setExtPlayerOpened(true)}
          >
            外部播放器
          </Button>
          {/* 加入预生成（FR-77）：选预设把本媒体加入预生成队列预热首播 */}
          <Button
            variant="subtle"
            color="gray"
            size="sm"
            leftSection={<IconMovie size={14} />}
            onClick={() => setPregenOpened(true)}
          >
            加入预生成
          </Button>
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

      {/* 播放器：协商出 fMP4 时交描述符给自适应播放器（FR-53）；
          否则探测到 HLS 不可用时降级到 /api/play/:id/stream（非 ABR，浏览器原生 video）。 */}
      {playerUrl && playerIsABR !== null && (
        <VideoPlayer
          url={playerUrl}
          descriptor={descriptor ?? undefined}
          autoPlay
          subtitleEntries={subtitleEntries}
          subtitleVisible={subtitleVisible}
          isABR={playerIsABR}
          streamType={playerIsABR ? 'mpegts' : 'mp4'}
          initialPosition={media.last_position}
          onPositionReport={handlePositionReport}
          onEnded={handleEnded}
        />
      )}

      {/* 媒体信息 */}
      <Paper withBorder p="md" radius="md" bg="var(--mantine-color-default)">
        <Text fw={600} size="sm" mb="sm">媒体信息</Text>
        <Group gap="md" wrap="wrap">
          <div>
            <Text size="xs" c="dimmed">真实文件名</Text>
            <Text size="sm" truncate maw={400}>{media.file_name}</Text>
          </div>
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

      {/* 改显示名（仅库内，不动磁盘）：二次确认弹窗 */}
      <NameEditModal
        opened={nameEditKind === 'display'}
        title="修改显示名"
        label="显示名"
        message="仅修改系统内的展示名称，不会改动磁盘上的真实文件。留空则清除显示名、回退为真实文件名。"
        initialValue={media.display_name || ''}
        allowEmpty
        loading={nameEditSaving}
        onConfirm={confirmNameEdit}
        onCancel={() => setNameEditKind(null)}
      />

      {/* 改真实文件名（磁盘改名）：二次确认弹窗 */}
      <NameEditModal
        opened={nameEditKind === 'real'}
        title="修改真实文件名"
        label="真实文件名"
        message="将直接重命名磁盘上的文件，此操作会改动真实文件名。请确认无误后再继续。"
        initialValue={media.file_name}
        loading={nameEditSaving}
        onConfirm={confirmNameEdit}
        onCancel={() => setNameEditKind(null)}
      />

      {/* 分享链接弹窗（FR-43） */}
      <ShareDialog
        opened={shareOpened}
        onClose={() => setShareOpened(false)}
        resourceType="media"
        resourceID={media.id}
        title="分享此媒体"
      />

      {/* 外部播放器深链弹窗（FR-79）：续播点取自 media.last_position */}
      <ExternalPlayerDialog
        opened={extPlayerOpened}
        onClose={() => setExtPlayerOpened(false)}
        mediaID={media.id}
        lastPosition={media.last_position}
      />

      {/* 加入预生成队列弹窗（FR-77）：选预设把本媒体加入转码预生成队列 */}
      <PregenDialog
        opened={pregenOpened}
        onClose={() => setPregenOpened(false)}
        mediaID={media.id}
      />
    </Stack>
  )
}
