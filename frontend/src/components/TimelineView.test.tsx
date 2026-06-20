import { render } from '@testing-library/react'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import TimelineView from './TimelineView'
import type { MediaFile } from '@/types'

// 模拟 IntersectionObserver，捕获回调以手动触发“滚到底部哨兵进入视口”
let ioCb: ((entries: Array<{ isIntersecting: boolean }>) => void) | null = null
class MockIO {
  constructor(cb: (entries: Array<{ isIntersecting: boolean }>) => void) { ioCb = cb }
  observe() {}
  unobserve() {}
  disconnect() {}
}

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
        mediaFiles={[file(1), file(2)]}
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

describe('TimelineView 滚动加载', () => {
  beforeEach(() => {
    ioCb = null
    ;(globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO
  })

  it('底部哨兵进入视口且 hasMore 时触发 onLoadMore', () => {
    const onLoadMore = vi.fn()
    renderView({ onLoadMore, hasMore: true })
    expect(typeof ioCb).toBe('function') // 哨兵已注册观察
    ioCb!([{ isIntersecting: true }])
    expect(onLoadMore).toHaveBeenCalled()
  })

  it('hasMore 为 false 时不触发 onLoadMore', () => {
    const onLoadMore = vi.fn()
    renderView({ onLoadMore, hasMore: false })
    if (ioCb) ioCb([{ isIntersecting: true }])
    expect(onLoadMore).not.toHaveBeenCalled()
  })
})
