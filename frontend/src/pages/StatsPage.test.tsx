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

  it('热力方块与时间线条用品牌紫阶（FR-101 可视化精修）', async () => {
    server.use(http.get('*/api/library/stats', () => HttpResponse.json(sampleStats)))
    renderPage()
    // 有值的热力方块背景用品牌紫 token（color-mix purple-6），非写死 rgba 魔法值
    const cell0 = await screen.findByTestId('heat-cell-0')
    expect(cell0.style.background).toContain('--mantine-color-purple-6')
    expect(cell0.style.background).toContain('color-mix')
    // 时间线条以品牌锚点 purple-6 着色
    const bar = screen.getByLabelText('2026-06-24 观看 3 个')
    const fill = bar.querySelector('div') as HTMLElement
    expect(fill.style.background).toContain('--mantine-color-purple-6')
  })

  it('加载失败时提示错误', async () => {
    server.use(http.get('*/api/library/stats', () => HttpResponse.json({ message: '炸了' }, { status: 500 })))
    renderPage()
    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalled()
    })
  })

  it('空库（total=0）显示整页引导且不渲染任何图表卡', async () => {
    const emptyStats: WatchStats = {
      total: 0, watched: 0, unwatched: 0,
      recent_timeline: [],
      position_heatmap: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
      by_library: [], by_format: [], top_viewed: [],
    }
    server.use(http.get('*/api/library/stats', () => HttpResponse.json(emptyStats)))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('还没有可统计的媒体')).toBeVisible()
    })
    // 整页引导含一键 CTA
    expect(screen.getByRole('button', { name: /去添加媒体库/ })).toBeVisible()
    // 不渲染任何空图表卡：热力网格与进度概览卡都不出现
    expect(screen.queryByTestId('heat-cell-0')).toBeNull()
    expect(screen.queryByText('观看进度概览')).toBeNull()
  })

  it('稀疏态（有媒体但无任何观看活动）隐藏满 0 热力与孤立时间线', async () => {
    const sparseStats: WatchStats = {
      total: 8, watched: 0, unwatched: 8,
      recent_timeline: [],
      position_heatmap: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
      by_library: [{ library_id: 1, label: '电影', watched: 0 }],
      by_format: [{ format: 'mp4', watched: 0 }],
      top_viewed: [],
    }
    server.use(http.get('*/api/library/stats', () => HttpResponse.json(sparseStats)))
    renderPage()
    // 仍渲染进度概览（有总数）
    await waitFor(() => {
      expect(screen.getByText('观看进度概览')).toBeVisible()
    })
    // 续播热力卡与时间线卡整卡隐藏（满 0 无意义）
    expect(screen.queryByText('续播位置热力')).toBeNull()
    expect(screen.queryByText('最近观看时间线')).toBeNull()
    expect(screen.queryByTestId('heat-cell-0')).toBeNull()
  })
})
