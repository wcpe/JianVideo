import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MantineProvider } from '@mantine/core'
import TimelineScrubber from './TimelineScrubber'
import type { DateGroup } from '@/utils/timeline'

/** 构造一个分组，date 为分组键，files 含若干项用于浮层九宫格缩略图 */
function makeGroup(date: string, ids: number[]): DateGroup {
  return {
    date,
    files: ids.map((id) => ({
      id, library_id: 1,
      file_path: `D:\\Photos\\${id}.jpg`, file_name: `${id}.jpg`,
      file_size: 0, format: 'jpg',
      video_codec: '', audio_codec: '', duration: 0,
      width: 0, height: 0, bitrate: 0, subtitle_tracks: '',
      added_at: `${date}T00:00:00Z`, modified_at: `${date}T00:00:00Z`,
    })),
  }
}

const groups: DateGroup[] = [
  makeGroup('2025-03-15', [1, 11, 12]), // 顶部=最新（3 张，用于九宫格）
  makeGroup('2025-02-10', [2]),
  makeGroup('2025-01-05', [3]),
  makeGroup('2024-12-20', [4]),
  makeGroup('2024-11-01', [5]), // 底部=最旧
]

function renderScrubber(onSeek = vi.fn()) {
  const utils = render(
    <MantineProvider>
      <TimelineScrubber groups={groups} onSeek={onSeek} />
    </MantineProvider>,
  )
  const slider = screen.getByRole('slider', { name: '时间轴拖动定位' })
  // jsdom 无真实布局：桩化轨道尺寸（顶部 y=0、高度 500）
  vi.spyOn(slider, 'getBoundingClientRect').mockReturnValue({
    top: 0, left: 0, right: 12, bottom: 500, width: 12, height: 500,
    x: 0, y: 0, toJSON: () => ({}),
  } as DOMRect)
  // jsdom 未实现指针捕获 API，桩化避免抛错
  ;(slider as unknown as { setPointerCapture: () => void }).setPointerCapture = () => {}
  ;(slider as unknown as { releasePointerCapture: () => void }).releasePointerCapture = () => {}
  return { ...utils, slider, onSeek }
}

describe('TimelineScrubber', () => {
  beforeEach(() => vi.clearAllMocks())

  it('拖动到顶部预览最新分组，松手 onSeek(0)', () => {
    const { slider, onSeek } = renderScrubber()
    // 按下并移动到顶部（clientY=0 → fraction=0 → 下标 0）
    fireEvent.pointerDown(slider, { clientY: 0, pointerId: 1 })
    fireEvent.pointerMove(slider, { clientY: 0, pointerId: 1 })
    // 浮层显示最新分组日期
    expect(screen.getByText('2025-03-15')).toBeInTheDocument()
    fireEvent.pointerUp(slider, { clientY: 0, pointerId: 1 })
    expect(onSeek).toHaveBeenCalledWith(0)
  })

  it('拖动到底部预览最旧分组，松手 onSeek(最后一组)', () => {
    const { slider, onSeek } = renderScrubber()
    // clientY=500 → fraction=1 → 下标 4
    fireEvent.pointerDown(slider, { clientY: 500, pointerId: 1 })
    fireEvent.pointerMove(slider, { clientY: 500, pointerId: 1 })
    expect(screen.getByText('2024-11-01')).toBeInTheDocument()
    fireEvent.pointerUp(slider, { clientY: 500, pointerId: 1 })
    expect(onSeek).toHaveBeenCalledWith(4)
  })

  it('拖动到中部预览对应分组', () => {
    const { slider } = renderScrubber()
    // clientY=250 → fraction=0.5 → 5 组均分落下标 2
    fireEvent.pointerDown(slider, { clientY: 250, pointerId: 1 })
    fireEvent.pointerMove(slider, { clientY: 250, pointerId: 1 })
    expect(screen.getByText('2025-01-05')).toBeInTheDocument()
  })

  it('未交互时不渲染浮层', () => {
    renderScrubber()
    expect(screen.queryByText('2025-03-15')).not.toBeInTheDocument()
  })

  // FR-120：hover（未按下）即在指针处弹出该时段预览
  it('hover 移动（未按下）即弹出该时段预览，移出隐藏', () => {
    const { slider } = renderScrubber()
    // 指针进入并在顶部移动（未 pointerDown）→ 弹出最新分组预览
    fireEvent.pointerMove(slider, { clientY: 0, pointerId: 1 })
    expect(screen.getByText('2025-03-15')).toBeInTheDocument()
    // 浮层带数量与无障碍标签（日期 + 数量）
    expect(screen.getByRole('img', { name: /2025-03-15.*3/ })).toBeInTheDocument()
    // 移出轨道 → 预览隐藏
    fireEvent.pointerLeave(slider)
    expect(screen.queryByText('2025-03-15')).not.toBeInTheDocument()
  })

  it('hover 到底部预览最旧分组', () => {
    const { slider } = renderScrubber()
    fireEvent.pointerMove(slider, { clientY: 500, pointerId: 1 })
    expect(screen.getByText('2024-11-01')).toBeInTheDocument()
  })

  it('方向键下移一个分组并触发 onSeek', () => {
    const { slider, onSeek } = renderScrubber()
    slider.focus()
    fireEvent.keyDown(slider, { key: 'ArrowDown' })
    // 从默认 0（顶部/最新）下移一格到下标 1
    expect(onSeek).toHaveBeenCalledWith(1)
  })

  it('空分组不渲染滑块', () => {
    render(
      <MantineProvider>
        <TimelineScrubber groups={[]} onSeek={vi.fn()} />
      </MantineProvider>,
    )
    expect(screen.queryByRole('slider')).not.toBeInTheDocument()
  })
})
