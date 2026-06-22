import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Modal, Group, Stack, Button, ActionIcon, Text, Box, ScrollArea, Divider, Tooltip, Anchor } from '@mantine/core'
import {
  IconChevronLeft, IconChevronRight, IconX, IconMaximize, IconMinimize,
  IconDownload, IconPlayerPlay,
} from '@tabler/icons-react'
import MediaThumbnail from '@/components/MediaThumbnail'
import { isImageFile, mediaDisplayName } from '@/utils/media'
import { formatSize, formatDuration } from '@/utils/format'
import type { MediaFile } from '@/types'

interface MediaDetailPanelProps {
  files: MediaFile[]
  /** 选中项在 files 中的下标；null 表示面板关闭 */
  initialIndex: number | null
  onClose: () => void
  customImageExtensions: Record<number, string[]>
}

const ZOOM_MIN = 1
const ZOOM_MAX = 4
const ZOOM_STEP = 0.25

/** 一行「标签：值」详情，值为空时不渲染 */
function DetailRow({ label, value }: { label: string; value: string | number | null | undefined }) {
  if (value === null || value === undefined || value === '') return null
  return (
    <Group justify="space-between" gap="sm" wrap="nowrap">
      <Text size="sm" c="dimmed" style={{ flexShrink: 0 }}>{label}</Text>
      <Text size="sm" ta="right" style={{ wordBreak: 'break-all' }}>{value}</Text>
    </Group>
  )
}

function formatTime(s?: string | null): string {
  if (!s) return ''
  const d = new Date(s)
  return isNaN(d.getTime()) ? '' : d.toLocaleString()
}

// 媒体时间来源中文标注（FR-38）
const MEDIA_TIME_SOURCE_LABEL: Record<string, string> = {
  exif: 'EXIF', filename: '文件名', created: '创建时间', modified: '修改时间',
}

/** 是否含可展示的 EXIF 信息（FR-38） */
function hasExif(f: MediaFile): boolean {
  return !!(f.media_time || f.camera || f.lens || f.aperture || f.shutter || (f.iso ?? 0) > 0 || (f.gps_lat ?? 0) !== 0 || (f.gps_lon ?? 0) !== 0)
}

/**
 * 文件详情面板（FR-34）：左侧预览（图片可滚轮缩放 / 视频缩略图 + 播放）、右侧元数据，
 * 支持全屏切换、←/→ 上下一项、Esc 关闭。EXIF 区块由 FR-38 在右侧补充。
 */
export default function MediaDetailPanel({ files, initialIndex, onClose, customImageExtensions }: MediaDetailPanelProps) {
  const navigate = useNavigate()
  const opened = initialIndex !== null
  const [idx, setIdx] = useState<number>(initialIndex ?? 0)
  const [fullscreen, setFullscreen] = useState(false)
  const [zoom, setZoom] = useState(1)

  // 重新打开（initialIndex 变化）时定位并复位缩放
  useEffect(() => {
    if (initialIndex !== null) {
      setIdx(initialIndex)
      setZoom(1)
    }
  }, [initialIndex])

  const goPrev = useCallback(() => setIdx(i => Math.max(0, i - 1)), [])
  const goNext = useCallback(() => setIdx(i => Math.min(files.length - 1, i + 1)), [files.length])

  // 换项复位缩放
  useEffect(() => { setZoom(1) }, [idx])

  // 键盘导航：←/→ 上下一项，Esc 关闭
  useEffect(() => {
    if (!opened) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft') goPrev()
      else if (e.key === 'ArrowRight') goNext()
      else if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [opened, goPrev, goNext, onClose])

  const file = opened && files[idx] ? files[idx] : null
  if (!file) return null

  const isImage = isImageFile(file, customImageExtensions)

  const handleWheel = (e: React.WheelEvent) => {
    e.preventDefault()
    setZoom(z => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z + (e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP))))
  }

  return (
    <Modal
      opened={opened}
      onClose={onClose}
      fullScreen={fullscreen}
      size="90%"
      padding="md"
      withCloseButton={false}
      title={
        <Group justify="space-between" w="100%" wrap="nowrap">
          <Text fw={600} truncate>{mediaDisplayName(file)}</Text>
          <Group gap={4} wrap="nowrap">
            <Tooltip label="上一项 (←)">
              <ActionIcon variant="subtle" onClick={goPrev} disabled={idx <= 0} aria-label="上一项">
                <IconChevronLeft size={18} />
              </ActionIcon>
            </Tooltip>
            <Tooltip label="下一项 (→)">
              <ActionIcon variant="subtle" onClick={goNext} disabled={idx >= files.length - 1} aria-label="下一项">
                <IconChevronRight size={18} />
              </ActionIcon>
            </Tooltip>
            <Tooltip label={fullscreen ? '退出全屏' : '全屏'}>
              <ActionIcon variant="subtle" onClick={() => setFullscreen(f => !f)} aria-label="全屏切换">
                {fullscreen ? <IconMinimize size={18} /> : <IconMaximize size={18} />}
              </ActionIcon>
            </Tooltip>
            <ActionIcon variant="subtle" color="gray" onClick={onClose} aria-label="关闭">
              <IconX size={18} />
            </ActionIcon>
          </Group>
        </Group>
      }
      styles={{ title: { width: '100%' } }}
    >
      <Group align="stretch" gap="md" wrap="nowrap" style={{ minHeight: fullscreen ? '80vh' : '60vh' }}>
        {/* 左侧预览 */}
        <Box style={{ flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', background: '#000', borderRadius: 8, overflow: 'hidden' }}>
          {isImage ? (
            <Box
              onWheel={handleWheel}
              style={{ width: '100%', height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', overflow: 'hidden', cursor: zoom > 1 ? 'zoom-out' : 'zoom-in' }}
            >
              <Box
                component="img"
                src={`/api/library/media/${file.id}/raw`}
                alt={file.file_name}
                style={{ maxWidth: '100%', maxHeight: fullscreen ? '78vh' : '58vh', objectFit: 'contain', transform: `scale(${zoom})`, transition: 'transform 0.1s ease-out' }}
              />
            </Box>
          ) : (
            <Stack align="center" gap="md" p="xl">
              <Box w={320} maw="100%">
                <MediaThumbnail mediaID={file.id} fileName={file.file_name} />
              </Box>
              <Button leftSection={<IconPlayerPlay size={16} />} onClick={() => navigate(`/play/${file.id}`)}>
                打开播放
              </Button>
            </Stack>
          )}
        </Box>

        {/* 右侧详情 */}
        <ScrollArea style={{ width: 300, flexShrink: 0 }} type="hover">
          <Stack gap="xs">
            <Text fw={600} size="sm">文件信息</Text>
            <DetailRow label="显示名" value={mediaDisplayName(file)} />
            <DetailRow label="文件名" value={file.file_name} />
            <DetailRow label="类型" value={file.format?.toUpperCase()} />
            <DetailRow label="大小" value={formatSize(file.file_size)} />
            {file.width > 0 && file.height > 0 && <DetailRow label="分辨率" value={`${file.width} × ${file.height}`} />}
            {!isImage && <DetailRow label="时长" value={formatDuration(file.duration)} />}
            {!isImage && <DetailRow label="视频编码" value={file.video_codec} />}
            {!isImage && <DetailRow label="音频编码" value={file.audio_codec} />}
            <Divider my={4} />
            <DetailRow label="加入时间" value={formatTime(file.added_at)} />
            <DetailRow label="修改时间" value={formatTime(file.modified_at)} />

            {/* EXIF 信息（FR-38）：有数据才展示 */}
            {hasExif(file) && (
              <>
                <Divider my={4} label="EXIF" labelPosition="left" />
                <DetailRow
                  label="拍摄时间"
                  value={file.media_time ? `${formatTime(file.media_time)}${file.media_time_source ? `（${MEDIA_TIME_SOURCE_LABEL[file.media_time_source] ?? file.media_time_source}）` : ''}` : ''}
                />
                <DetailRow label="相机" value={file.camera} />
                <DetailRow label="镜头" value={file.lens} />
                <DetailRow label="光圈" value={file.aperture} />
                <DetailRow label="快门" value={file.shutter} />
                <DetailRow label="ISO" value={(file.iso ?? 0) > 0 ? file.iso : ''} />
                {((file.gps_lat ?? 0) !== 0 || (file.gps_lon ?? 0) !== 0) && (
                  <>
                    <DetailRow label="GPS" value={`${file.gps_lat?.toFixed(6)}, ${file.gps_lon?.toFixed(6)}`} />
                    <Group justify="flex-end">
                      <Anchor
                        href={`https://www.openstreetmap.org/?mlat=${file.gps_lat}&mlon=${file.gps_lon}#map=15/${file.gps_lat}/${file.gps_lon}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        size="sm"
                      >
                        在外部地图打开
                      </Anchor>
                    </Group>
                  </>
                )}
              </>
            )}

            <Divider my="sm" />
            <Button
              component="a"
              href={`/api/library/media/${file.id}/download`}
              download
              variant="light"
              leftSection={<IconDownload size={16} />}
              fullWidth
            >
              下载原文件
            </Button>
          </Stack>
        </ScrollArea>
      </Group>
    </Modal>
  )
}
