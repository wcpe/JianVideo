import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import TimelineView from './TimelineView'
import type { MediaFile } from '@/types'

// jsdom 无 IntersectionObserver，提供空实现避免组件挂载哨兵时崩溃
class MockIO {
  observe() {}
  unobserve() {}
  disconnect() {}
}

// 三个同日期媒体，保证落在同一可见分组里
const file = (id: number): MediaFile => ({
  id, library_id: 1, file_path: `/x/f${id}.png`, file_name: `f${id}.png`,
  file_size: 1, format: 'png', video_codec: '', audio_codec: '', duration: 0,
  width: 1, height: 1, bitrate: 0, subtitle_tracks: '',
  added_at: '2025-01-09T00:00:00Z', modified_at: '2025-01-09T00:00:00Z',
})

function renderView(props: Partial<React.ComponentProps<typeof TimelineView>> = {}) {
  return render(
    <MantineProvider>
      <TimelineView
        mediaFiles={[file(1), file(2), file(3)]}
        loading={false}
        error={null}
        customImageExtensions={{}}
        onErrorClose={() => {}}
        onOpenFile={() => {}}
        {...props}
      />
    </MantineProvider>,
  )
}

// 取某卡片（按文件名）的选中态
function cardSelected(name: string): boolean {
  const card = screen.getByText(name).closest('.mantine-Card-root')
  return card?.getAttribute('data-selected') === 'true'
}

describe('TimelineView 多选与右键批量（FR-69）', () => {
  beforeEach(() => {
    ;(globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO
  })

  it('普通单击打开预览（画廊式）、Ctrl 点选才进入多选并上抛', () => {
    const onSelectionChange = vi.fn()
    const onOpenFile = vi.fn()
    renderView({ onSelectionChange, onOpenFile })
    // 普通单击=打开，不选中
    fireEvent.click(screen.getByText('f1.png'))
    expect(onOpenFile).toHaveBeenCalledWith(expect.objectContaining({ id: 1 }))
    expect(cardSelected('f1.png')).toBe(false)
    // Ctrl 点选两张进入多选
    fireEvent.click(screen.getByText('f1.png'), { ctrlKey: true })
    fireEvent.click(screen.getByText('f3.png'), { ctrlKey: true })
    expect(cardSelected('f1.png')).toBe(true)
    expect(cardSelected('f3.png')).toBe(true)
    const last = onSelectionChange.mock.calls.at(-1)![0] as number[]
    expect([...last].sort((a, b) => a - b)).toEqual([1, 3])
  })

  it('Shift 区间跨卡连选（全部已加载项范围内）', () => {
    renderView({ onSelectionChange: vi.fn() })
    fireEvent.click(screen.getByText('f1.png'), { ctrlKey: true })
    fireEvent.click(screen.getByText('f3.png'), { shiftKey: true })
    expect(cardSelected('f1.png')).toBe(true)
    expect(cardSelected('f2.png')).toBe(true)
    expect(cardSelected('f3.png')).toBe(true)
  })

  it('右键「删除选中」按选中批量调 onBatchDelete', async () => {
    const onBatchDelete = vi.fn()
    renderView({ onBatchDelete })
    fireEvent.click(screen.getByText('f1.png'), { ctrlKey: true })
    fireEvent.click(screen.getByText('f2.png'), { ctrlKey: true })
    fireEvent.contextMenu(screen.getByText('f2.png'))
    await userEvent.click(await screen.findByText('删除选中（2）'))
    expect(onBatchDelete).toHaveBeenCalledWith([1, 2])
  })

  it('右键「全选」选中全部已加载项', async () => {
    const onSelectionChange = vi.fn()
    renderView({ onSelectionChange })
    fireEvent.contextMenu(screen.getByText('f1.png'))
    await userEvent.click(await screen.findByText('全选'))
    const last = onSelectionChange.mock.calls.at(-1)![0] as number[]
    expect([...last].sort((a, b) => a - b)).toEqual([1, 2, 3])
  })

  it('未提供选择回调时点击仅触发 onOpenFile、不进入选择态', () => {
    const onOpenFile = vi.fn()
    renderView({ onOpenFile })
    fireEvent.click(screen.getByText('f1.png'))
    expect(onOpenFile).toHaveBeenCalledWith(expect.objectContaining({ id: 1 }))
    expect(cardSelected('f1.png')).toBe(false)
  })
})
