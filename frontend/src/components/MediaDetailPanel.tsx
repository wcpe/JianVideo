import { useState, useEffect, useCallback, useRef, Suspense, lazy } from 'react'
import { Modal, Group, Stack, Button, ActionIcon, Text, Box, ScrollArea, Divider, Tooltip, Anchor } from '@mantine/core'
import {
  IconChevronLeft, IconChevronRight, IconX, IconMaximize, IconMinimize,
  IconDownload, IconRotateClockwise, IconRotate2, IconPlayerPlay, IconPlayerPause,
} from '@tabler/icons-react'
// FR-102：懒加载 VideoPlayer，仅在灯箱内实际查看视频时才加载其 mpegts.js 等重内核，
// 避免图片预览场景白白拉入大体积播放内核。
const VideoPlayer = lazy(() => import('@/components/VideoPlayer'))
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
// 双击放大目标倍率（FR-105）：1×↔2× 切换
const ZOOM_DOUBLE_CLICK = 2
// 幻灯片自动切换间隔（毫秒，FR-105）
const SLIDESHOW_INTERVAL_MS = 3000
// 相邻预加载半径（FR-105）：预取前后各 1 张原图，切换不闪白
const PRELOAD_RADIUS = 1

/** 原图地址（FR-105 预加载与图片预览共用） */
function rawUrl(mediaID: number): string {
  return `/api/library/media/${mediaID}/raw`
}

/** 视频流播放地址（FR-102）：绝对化避免 mpegts.js 在 Web Worker 中 fetch 相对 URL 失败 */
function streamUrl(mediaID: number): string {
  return new URL(`/api/play/${mediaID}/stream`, window.location.href).toString()
}

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
 * 文件详情面板（FR-34）：左侧预览（图片可滚轮缩放 / 视频内嵌播放器直接播放，FR-102）、右侧元数据，
 * 支持全屏切换、←/→ 上下一项、Esc 关闭。EXIF 区块由 FR-38 在右侧补充。
 */
export default function MediaDetailPanel({ files, initialIndex, onClose, customImageExtensions }: MediaDetailPanelProps) {
  const opened = initialIndex !== null
  const [idx, setIdx] = useState<number>(initialIndex ?? 0)
  const [fullscreen, setFullscreen] = useState(false)
  const [zoom, setZoom] = useState(1)
  // 平移量（FR-105）：放大后拖拽查看图片别处
  const [pan, setPan] = useState({ x: 0, y: 0 })
  // 旋转角度（FR-105）：0/90/180/270，左右各 90°
  const [rotation, setRotation] = useState(0)
  // 幻灯片自动轮播开关（FR-105）
  const [slideshow, setSlideshow] = useState(false)

  // 鼠标 / 触摸拖拽平移的过程态（FR-105）：起点坐标与起始平移量，拖拽中不触发渲染
  const dragRef = useRef<{ active: boolean; startX: number; startY: number; panX: number; panY: number }>({
    active: false, startX: 0, startY: 0, panX: 0, panY: 0,
  })
  // 双指捏合缩放的过程态（FR-105）：起始两指间距与起始缩放
  const pinchRef = useRef<{ active: boolean; startDist: number; startZoom: number }>({
    active: false, startDist: 0, startZoom: 1,
  })

  const total = files.length

  // 重新打开（initialIndex 变化）时定位并复位查看状态
  useEffect(() => {
    if (initialIndex !== null) {
      setIdx(initialIndex)
      setZoom(1)
      setPan({ x: 0, y: 0 })
      setRotation(0)
      setSlideshow(false)
    }
  }, [initialIndex])

  // 上/下一项环绕循环（FR-105）：到头时首↔尾循环
  const goPrev = useCallback(() => {
    setIdx(i => (total > 0 ? (i - 1 + total) % total : i))
  }, [total])
  const goNext = useCallback(() => {
    setIdx(i => (total > 0 ? (i + 1) % total : i))
  }, [total])

  // 换项复位缩放 / 平移 / 旋转（FR-105）
  useEffect(() => {
    setZoom(1)
    setPan({ x: 0, y: 0 })
    setRotation(0)
  }, [idx])

  // 右/左旋转 90°（FR-105）
  const rotateRight = useCallback(() => setRotation(r => (r + 90) % 360), [])
  const rotateLeft = useCallback(() => setRotation(r => (r + 270) % 360), [])

  // 键盘导航与快捷键（FR-105 在既有 ←/→/Esc 上补 +/-、F、Space、R）
  useEffect(() => {
    if (!opened) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft') goPrev()
      else if (e.key === 'ArrowRight') goNext()
      else if (e.key === 'Escape') onClose()
      else if (e.key === '+' || e.key === '=') setZoom(z => Math.min(ZOOM_MAX, z + ZOOM_STEP))
      else if (e.key === '-' || e.key === '_') setZoom(z => Math.max(ZOOM_MIN, z - ZOOM_STEP))
      else if (e.key === 'f' || e.key === 'F') setFullscreen(v => !v)
      else if (e.key === ' ') { e.preventDefault(); goNext() }
      else if (e.key === 'r' || e.key === 'R') rotateRight()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [opened, goPrev, goNext, onClose, rotateRight])

  // 幻灯片自动轮播（FR-105）：开启时按固定间隔自动切下一张
  useEffect(() => {
    if (!opened || !slideshow) return
    const timer = window.setInterval(goNext, SLIDESHOW_INTERVAL_MS)
    return () => window.clearInterval(timer)
  }, [opened, slideshow, goNext])

  // 相邻原图预加载（FR-105）：预取当前项前后各 PRELOAD_RADIUS 张，切换不闪白
  useEffect(() => {
    if (!opened || total === 0) return
    for (let d = 1; d <= PRELOAD_RADIUS; d++) {
      for (const j of [(idx - d + total) % total, (idx + d) % total]) {
        const neighbor = files[j]
        if (neighbor && isImageFile(neighbor, customImageExtensions)) {
          // 用 createElement('img') 触发浏览器原图预取，jsdom 下亦可断言 src 赋值
          const pre = document.createElement('img')
          pre.src = rawUrl(neighbor.id)
        }
      }
    }
  }, [opened, idx, total, files, customImageExtensions])

  const file = opened && files[idx] ? files[idx] : null
  if (!file) return null

  const isImage = isImageFile(file, customImageExtensions)

  const handleWheel = (e: React.WheelEvent) => {
    e.preventDefault()
    setZoom(z => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z + (e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP))))
  }

  // 双击放大 / 复位（FR-105）：1×↔2× 切换，复位时平移归零
  const handleDoubleClick = () => {
    if (zoom > 1) {
      setZoom(1)
      setPan({ x: 0, y: 0 })
    } else {
      setZoom(ZOOM_DOUBLE_CLICK)
    }
  }

  // 鼠标按下开始拖拽平移（FR-105）：仅放大态生效，过程绑定到 window 以便拖出元素仍跟手
  const handleMouseDown = (e: React.MouseEvent) => {
    if (zoom <= 1) return
    e.preventDefault()
    dragRef.current = { active: true, startX: e.clientX, startY: e.clientY, panX: pan.x, panY: pan.y }
    const onMove = (ev: MouseEvent) => {
      if (!dragRef.current.active) return
      setPan({
        x: dragRef.current.panX + (ev.clientX - dragRef.current.startX),
        y: dragRef.current.panY + (ev.clientY - dragRef.current.startY),
      })
    }
    const onUp = () => {
      dragRef.current.active = false
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  // 触摸开始（FR-105）：单指准备平移，双指准备捏合缩放
  const handleTouchStart = (e: React.TouchEvent) => {
    if (e.touches.length === 2) {
      const dx = e.touches[0].clientX - e.touches[1].clientX
      const dy = e.touches[0].clientY - e.touches[1].clientY
      pinchRef.current = { active: true, startDist: Math.hypot(dx, dy), startZoom: zoom }
    } else if (e.touches.length === 1 && zoom > 1) {
      const t = e.touches[0]
      dragRef.current = { active: true, startX: t.clientX, startY: t.clientY, panX: pan.x, panY: pan.y }
    }
  }

  // 触摸移动（FR-105）：双指改缩放，单指改平移
  const handleTouchMove = (e: React.TouchEvent) => {
    if (pinchRef.current.active && e.touches.length === 2) {
      e.preventDefault()
      const dx = e.touches[0].clientX - e.touches[1].clientX
      const dy = e.touches[0].clientY - e.touches[1].clientY
      const ratio = Math.hypot(dx, dy) / (pinchRef.current.startDist || 1)
      setZoom(Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, pinchRef.current.startZoom * ratio)))
    } else if (dragRef.current.active && e.touches.length === 1) {
      e.preventDefault()
      const t = e.touches[0]
      setPan({
        x: dragRef.current.panX + (t.clientX - dragRef.current.startX),
        y: dragRef.current.panY + (t.clientY - dragRef.current.startY),
      })
    }
  }

  // 触摸结束（FR-105）：松手清理过程态，缩回 1× 时平移归零
  const handleTouchEnd = (e: React.TouchEvent) => {
    if (e.touches.length === 0) {
      dragRef.current.active = false
      pinchRef.current.active = false
      if (zoom <= 1) setPan({ x: 0, y: 0 })
    }
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
          <Group gap="xs" wrap="nowrap" style={{ minWidth: 0 }}>
            <Text fw={600} truncate>{mediaDisplayName(file)}</Text>
            {/* 位置计数（FR-105）：当前 / 总数 */}
            <Text size="sm" c="dimmed" style={{ flexShrink: 0 }}>{idx + 1} / {total}</Text>
          </Group>
          <Group gap={4} wrap="nowrap">
            <Tooltip label="上一项 (←)">
              <ActionIcon variant="subtle" onClick={goPrev} aria-label="上一项">
                <IconChevronLeft size={18} />
              </ActionIcon>
            </Tooltip>
            <Tooltip label="下一项 (→)">
              <ActionIcon variant="subtle" onClick={goNext} aria-label="下一项">
                <IconChevronRight size={18} />
              </ActionIcon>
            </Tooltip>
            {/* 图片专属：旋转与幻灯片（FR-105），视频分支不显示 */}
            {isImage && (
              <>
                <Tooltip label="向左旋转 (R 为右旋)">
                  <ActionIcon variant="subtle" onClick={rotateLeft} aria-label="向左旋转">
                    <IconRotate2 size={18} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label="向右旋转 (R)">
                  <ActionIcon variant="subtle" onClick={rotateRight} aria-label="向右旋转">
                    <IconRotateClockwise size={18} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label={slideshow ? '暂停幻灯片' : '开始幻灯片'}>
                  <ActionIcon variant="subtle" onClick={() => setSlideshow(v => !v)} aria-label="幻灯片切换">
                    {slideshow ? <IconPlayerPause size={18} /> : <IconPlayerPlay size={18} />}
                  </ActionIcon>
                </Tooltip>
              </>
            )}
            <Tooltip label={fullscreen ? '退出全屏 (F)' : '全屏 (F)'}>
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
              onTouchStart={handleTouchStart}
              onTouchMove={handleTouchMove}
              onTouchEnd={handleTouchEnd}
              style={{ width: '100%', height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', overflow: 'hidden', touchAction: 'none', cursor: zoom > 1 ? (dragRef.current.active ? 'grabbing' : 'grab') : 'zoom-in' }}
            >
              <Box
                component="img"
                src={rawUrl(file.id)}
                alt={file.file_name}
                draggable={false}
                onDoubleClick={handleDoubleClick}
                onMouseDown={handleMouseDown}
                style={{
                  maxWidth: '100%',
                  maxHeight: fullscreen ? '78vh' : '58vh',
                  objectFit: 'contain',
                  // 合成变换（FR-105）：先平移、再缩放、再旋转；拖拽中关动画跟手
                  transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom}) rotate(${rotation}deg)`,
                  transition: dragRef.current.active ? 'none' : 'transform 0.1s ease-out',
                  userSelect: 'none',
                }}
              />
            </Box>
          ) : (
            // FR-102：视频内嵌 VideoPlayer 直接播放（静音自动播），去掉「打开播放」中间步骤。
            // 灯箱内用既有 /api/play/:id/stream 流地址（与播放页降级路径一致），不引入协商逻辑。
            <Box w="100%" px="md" style={{ maxWidth: 960 }}>
              <Suspense fallback={<Text c="dimmed" size="sm">加载播放器…</Text>}>
                <VideoPlayer url={streamUrl(file.id)} streamType="mp4" autoPlay />
              </Suspense>
            </Box>
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
