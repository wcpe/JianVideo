import { useRef, useState, useCallback, useEffect } from 'react'
import { Box, Paper, SimpleGrid, Text } from '@mantine/core'
import MediaThumbnail from '@/components/MediaThumbnail'
import { positionToGroupIndex } from '@/utils/timeline'
import type { DateGroup } from '@/utils/timeline'

interface TimelineScrubberProps {
  // 当前分组列表（已按 granularity 分组、倒序），顶部=最新、底部=最旧
  groups: DateGroup[]
  // 松手 / 方向键确定目标分组时回调，参数为目标分组下标
  onSeek: (index: number) => void
}

// 浮层九宫格展示的缩略图上限（前 N 张）
const PREVIEW_THUMBS = 6

/**
 * 时间轴时间标尺 scrubber（FR-68 + FR-120）：右侧竖向轨道。
 *
 * - hover（指针未按下）即按指针 Y 算目标分组、在指针处弹出预览浮层（分组日期 + 该组数量 +
 *   前若干张缩略图九宫格）；移出轨道隐藏（FR-120 苹果风）。
 * - 拖动（按下）持续更新预览并松手跳转到对应分组（FR-68 保留）。
 * - 键盘可达：上下方向键移动分组并跳转（FR-68 保留）。
 *
 * hover 与 drag 共用「指针 Y → 分组下标」纯函数 positionToGroupIndex；指针移动用 rAF 前沿节流，
 * 避免高频重渲染抖动。预览浮层 pointer-events:none + role="img" + aria-label（日期 + 数量）。
 */
export default function TimelineScrubber({ groups, onSeek }: TimelineScrubberProps) {
  const trackRef = useRef<HTMLDivElement>(null)
  // 预览下标（null 表示不显示浮层，hover 与 drag 共用）
  const [previewIndex, setPreviewIndex] = useState<number | null>(null)
  // 是否拖动中（指针已按下捕获）：用于区分 hover 与 drag，决定移出轨道时是否清预览
  const [dragging, setDragging] = useState(false)
  // 浮层纵向位置（指针在轨道内的像素 y），用于浮层贴着指针
  const [pointerY, setPointerY] = useState(0)

  // rAF 前沿节流：最新指针 Y 暂存于 ref，已排帧标记避免一帧内重复处理
  const latestY = useRef(0)
  const frame = useRef(0)

  const count = groups.length

  // 由指针 clientY 计算目标分组下标并更新预览（纯计算 + setState，无副作用泄漏）
  const applyFromPointer = useCallback((clientY: number): number => {
    const el = trackRef.current
    if (!el || count === 0) return 0
    const rect = el.getBoundingClientRect()
    const offset = clientY - rect.top
    const fraction = rect.height > 0 ? offset / rect.height : 0
    const index = positionToGroupIndex(fraction, count)
    setPreviewIndex(index)
    setPointerY(Math.min(rect.height, Math.max(0, offset)))
    return index
  }, [count])

  // rAF 前沿节流封装：首次（无待处理帧）立即处理保证响应即时，
  // 同帧内后续移动仅更新暂存 Y，由下一帧统一处理最新值，避免高频重渲染。
  const scheduleFromPointer = useCallback((clientY: number) => {
    latestY.current = clientY
    if (frame.current) return
    applyFromPointer(clientY)
    frame.current = requestAnimationFrame(() => {
      frame.current = 0
      // 帧间若指针又移动过，补处理最新值
      if (latestY.current !== clientY) applyFromPointer(latestY.current)
    })
  }, [applyFromPointer])

  // 卸载时清理待处理帧，避免泄漏
  useEffect(() => () => { if (frame.current) cancelAnimationFrame(frame.current) }, [])

  const handlePointerDown = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    // 捕获指针，拖出轨道范围仍持续跟手
    e.currentTarget.setPointerCapture?.(e.pointerId)
    setDragging(true)
    applyFromPointer(e.clientY)
  }, [applyFromPointer])

  const handlePointerMove = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    // hover 与 drag 均更新预览（拖动中跟手、未按下时随 hover 弹出）
    scheduleFromPointer(e.clientY)
  }, [scheduleFromPointer])

  const handlePointerUp = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    if (!dragging) return
    const index = applyFromPointer(e.clientY)
    e.currentTarget.releasePointerCapture?.(e.pointerId)
    setDragging(false)
    // 松手后回到 hover 态：指针仍在轨道上，保留预览不清空
    onSeek(index)
  }, [dragging, applyFromPointer, onSeek])

  // 指针移出轨道：非拖动态隐藏预览（拖动态由 pointerUp 收尾，跟手不中断）
  const handlePointerLeave = useCallback(() => {
    if (dragging) return
    setPreviewIndex(null)
  }, [dragging])

  // 键盘可达：上下方向键移动一个分组并跳转（顶部=最新=下标 0）
  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    if (count === 0) return
    const base = previewIndex ?? 0
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      const next = Math.min(count - 1, base + 1)
      setPreviewIndex(next)
      onSeek(next)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      const next = Math.max(0, base - 1)
      setPreviewIndex(next)
      onSeek(next)
    }
  }, [previewIndex, count, onSeek])

  if (count === 0) return null

  const activeGroup = previewIndex !== null ? groups[previewIndex] : null
  const previewThumbs = activeGroup ? activeGroup.files.slice(0, PREVIEW_THUMBS) : []

  return (
    <Box
      ref={trackRef}
      role="slider"
      aria-label="时间轴拖动定位"
      aria-valuemin={0}
      aria-valuemax={count - 1}
      aria-valuenow={previewIndex ?? 0}
      aria-valuetext={activeGroup?.date}
      tabIndex={0}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerLeave={handlePointerLeave}
      onKeyDown={handleKeyDown}
      style={{
        position: 'absolute',
        top: 0,
        right: -16,
        width: 12,
        height: '100%',
        cursor: 'ns-resize',
        borderRadius: 6,
        background: 'var(--mantine-color-default-border)',
        touchAction: 'none',
      }}
    >
      {/* 预览浮层（FR-120）：贴着指针纵向位置，展示目标分组日期 + 数量 + 前若干张缩略图九宫格。
          pointer-events:none 不挡轨道交互；role="img" + aria-label 暴露日期与数量给无障碍。 */}
      {activeGroup && (
        <Paper
          shadow="md"
          p="xs"
          radius="md"
          withBorder
          role="img"
          aria-label={`${activeGroup.date} 共 ${activeGroup.files.length} 项`}
          style={{
            position: 'absolute',
            right: 20,
            top: pointerY,
            transform: 'translateY(-50%)',
            width: 188,
            pointerEvents: 'none',
            background: 'var(--mantine-color-default)',
            zIndex: 10,
          }}
        >
          <Text fw={600} size="sm" ta="center">{activeGroup.date}</Text>
          <Text c="dimmed" size="xs" ta="center" mb={6}>{activeGroup.files.length} 项</Text>
          {/* 缩略图九宫格：3 列密铺，最多前 6 张 */}
          <SimpleGrid cols={3} spacing={4} verticalSpacing={4}>
            {previewThumbs.map((f) => (
              <Box key={f.id} style={{ aspectRatio: '1', overflow: 'hidden', borderRadius: 4 }}>
                <MediaThumbnail mediaID={f.id} fileName={f.file_name} objectFit="cover" />
              </Box>
            ))}
          </SimpleGrid>
        </Paper>
      )}
    </Box>
  )
}
