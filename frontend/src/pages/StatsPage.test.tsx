import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import StatsPage from './StatsPage'
import { server } from '@/mocks/beforeAll'
import type { WatchStats } from '@/types'

const mockNotificationShow = vi.fn()

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}))

const sampleStats: WatchStats = {
  total: 10,
  watched: 4,
  unwatched: 6,
  recent_timeline: [
    { date: '2026-06-24', count: 3 },
    { date: '2026-06-23', count: 1 },
  ],
  position_heatmap: [2, 0, 0, 0, 0, 1, 0, 0, 0, 3],
  by_library: [
    { library_id: 1, label: '电影', watched: 3 },
    { library_id: 2, label: '剧集', watched: 1 },
  ],
  by_format: [
    { format: 'mp4', watched: 3 },
    { format: 'mkv', watched: 1 },
  ],
  top_viewed: [
    { id: 11, library_id: 1, file_path: 'D:/a.mp4', file_name: '热门片.mp4', file_size: 1, format: 'mp4', video_codec: '', audio_codec: '', duration: 100, width: 0, height: 0, bitrate: 0, subtitle_tracks: '', added_at: '', modified_at: '', view_count: 5 },
  ],
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/stats']}>
        <StatsPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('StatsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染标题与观看进度概览', async () => {
    server.use(http.get('*/api/library/stats', () => HttpResponse.json(sampleStats)))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('观看统计')).toBeVisible()
    })
    // 已看 / 总数概览
    expect(screen.getByText(/已看 4 \/ 共 10/)).toBeVisible()
  })

  it('渲染续播位置热力 10 档', async () => {
    server.use(http.get('*/api/library/stats', () => HttpResponse.json(sampleStats)))
    renderPage()
    await waitFor(() => {
      expect(screen.getByTestId('heat-cell-0')).toBeInTheDocument()
    })
    // 10 档全部渲染
    for (let i = 0; i < 10; i++) {
      expect(screen.getByTestId(`heat-cell-${i}`)).toBeInTheDocument()
    }
  })

  it('渲染各库 / 各格式分布与 Top 榜', async () => {
    server.use(http.get('*/api/library/stats', () => HttpResponse.json(sampleStats)))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('电影')).toBeVisible()
    })
    expect(screen.getByText('剧集')).toBeVisible()
    expect(screen.getByText('mp4')).toBeVisible()
    // Top 榜成员
    expect(screen.getByText('热门片.mp4')).toBeVisible()
    expect(screen.getByText('5 次')).toBeVisible()
  })

  it('加载失败时提示错误', async () => {
    server.use(http.get('*/api/library/stats', () => HttpResponse.json({ message: '炸了' }, { status: 500 })))
    renderPage()
    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalled()
    })
  })
})
