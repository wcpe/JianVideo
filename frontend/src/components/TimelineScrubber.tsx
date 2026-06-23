import { useRef, useState, useCallback } from 'react'
import { Box, Paper, Text } from '@mantine/core'
import MediaThumbnail from '@/components/MediaThumbnail'
import { positionToGroupIndex } from '@/utils/timeline'
import type { DateGroup } from '@/utils/timeline'

interface TimelineScrubberProps {
  // 当前分组列表（已按 granularity 分组、倒序），顶部=最新、底部=最旧
  groups: DateGroup[]
  // 松手 / 方向键确定目标分组时回调，参数为目标分组下标
  onSeek: (index: number) => void
}

/**
 * 时间轴可拖动 scrubber（FR-68）：右侧竖向滑块，拖动浮层预览目标时间段日期与首项缩略图，
 * 松手滚动跳转到对应分组。位置→下标映射复用 positionToGroupIndex 纯函数。
 */
export default function TimelineScrubber({ groups, onSeek }: TimelineScrubberProps) {
  const trackRef = useRef<HTMLDivElement>(null)
  // 拖动中标记与当前预览下标（null 表示未拖动、不显示浮层）
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  // 浮层纵向位置（指针在轨道内的像素 y），用于浮层贴着指针
  const [pointerY, setPointerY] = useState(0)

  const count = groups.length

  // 由指针 clientY 计算目标分组下标并更新预览
  const updateFromPointer = useCallback((clientY: number) => {
    const el = trackRef.current
    if (!el || count === 0) return 0
    const rect = el.getBoundingClientRect()
    const offset = clientY - rect.top
    const fraction = rect.height > 0 ? offset / rect.height : 0
    const index = positionToGroupIndex(fraction, count)
    setActiveIndex(index)
    setPointerY(Math.min(rect.height, Math.max(0, offset)))
    return index
  }, [count])

  const handlePointerDown = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    // 捕获指针，拖出轨道范围仍持续跟手
    e.currentTarget.setPointerCapture?.(e.pointerId)
    updateFromPointer(e.clientY)
  }, [updateFromPointer])

  const handlePointerMove = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    // 仅在拖动中（已捕获指针）才更新
    if (activeIndex === null) return
    updateFromPointer(e.clientY)
  }, [activeIndex, updateFromPointer])

  const handlePointerUp = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    if (activeIndex === null) return
    const index = updateFromPointer(e.clientY)
    e.currentTarget.releasePointerCapture?.(e.pointerId)
    setActiveIndex(null)
    onSeek(index)
  }, [activeIndex, updateFromPointer, onSeek])

  // 键盘可达：上下方向键移动一个分组并跳转（顶部=最新=下标 0）
  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    if (count === 0) return
    const base = activeIndex ?? 0
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      const next = Math.min(count - 1, base + 1)
      setActiveIndex(next)
      onSeek(next)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      const next = Math.max(0, base - 1)
      setActiveIndex(next)
      onSeek(next)
    }
  }, [activeIndex, count, onSeek])

  if (count === 0) return null

  const activeGroup = activeIndex !== null ? groups[activeIndex] : null

  return (
    <Box
      ref={trackRef}
      role="slider"
      aria-label="时间轴拖动定位"
      aria-valuemin={0}
      aria-valuemax={count - 1}
      aria-valuenow={activeIndex ?? 0}
      aria-valuetext={activeGroup?.date}
      tabIndex={0}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
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
      {/* 拖动浮层：贴着指针纵向位置，展示目标分组日期 + 首项缩略图 */}
      {activeGroup && (
        <Paper
          shadow="md"
          p="xs"
          radius="md"
          withBorder
          style={{
            position: 'absolute',
            right: 20,
            top: pointerY,
            transform: 'translateY(-50%)',
            width: 140,
            pointerEvents: 'none',
            background: 'var(--mantine-color-default)',
            zIndex: 10,
          }}
        >
          <Text fw={600} size="sm" ta="center" mb={6}>{activeGroup.date}</Text>
          {activeGroup.files[0] && (
            <MediaThumbnail
              mediaID={activeGroup.files[0].id}
              fileName={activeGroup.files[0].file_name}
            />
          )}
        </Paper>
      )}
    </Box>
  )
}
