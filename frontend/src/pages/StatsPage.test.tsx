import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import StatsPage from './StatsPage'
import { server } from '@/mocks/beforeAll'
import type { WatchStats, LibrarySummary, MediaTrends } from '@/types'

// Recharts 在 jsdom 下 ResponsiveContainer 量到宽高为 0、不渲染图形且告警；
// 替换为固定尺寸容器，保证统计页内折线/sparkline 正常渲染、无未捕获告警致测试失败。
vi.mock('recharts', async () => {
  const actual = await vi.importActual<typeof import('recharts')>('recharts')
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
      <div style={{ width: 600, height: 300 }}>{children}</div>
    ),
  }
})

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

const sampleSummary: LibrarySummary = {
  total: 100,
  video_count: 60,
  image_count: 40,
  total_size: 53687091200,
  total_duration: 36000,
  library_count: 2,
  by_library: [
    { library_id: 1, label: '电影', media_count: 60, video_count: 60, image_count: 0, total_size: 50000000000, total_duration: 36000 },
    { library_id: 2, label: '照片', media_count: 40, video_count: 0, image_count: 40, total_size: 3687091200, total_duration: 0 },
  ],
}

const sampleTrends: MediaTrends = {
  media_added: [
    { date: '2026-05-01', count: 10, size: 1000000000, duration: 3000 },
    { date: '2026-05-03', count: 5, size: 500000000, duration: 1500 },
  ],
}

// 统一注册四个数据源的默认 handler（各用例可再 server.use 覆盖）
function useDefaultHandlers() {
  server.use(
    http.get('*/api/library/stats', () => HttpResponse.json(sampleStats)),
    http.get('*/api/library/summary', () => HttpResponse.json(sampleSummary)),
    http.get('*/api/library/trends', () => HttpResponse.json(sampleTrends)),
    http.get('*/api/library/continue-watching', () => HttpResponse.json({ items: [{ id: 1 }, { id: 2 }, { id: 3 }] })),
  )
}

function renderPage(initialPath = '/stats') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <StatsPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('StatsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // 基线：四个数据源都给默认 handler，避免未匹配请求告警；各用例可再 server.use 覆盖
    useDefaultHandlers()
  })

  it('渲染标题与默认观看 tab 的进度概览', async () => {
    useDefaultHandlers()
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('统计')).toBeVisible()
    })
    // 默认落 watch tab：已看 / 总数概览
    expect(screen.getByText(/已看 4 \/ 共 10/)).toBeVisible()
  })

  it('观看统计数组字段为 null 时不白屏（防御性兜底回归）', async () => {
    // 模拟后端稀疏态把切片回成 JSON null（修复前 .map 会整页白屏）
    server.use(
      http.get('*/api/library/stats', () => HttpResponse.json({
        total: 5, watched: 0, unwatched: 5,
        recent_timeline: null, position_heatmap: null,
        by_library: null, by_format: null, top_viewed: null,
      })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('统计')).toBeVisible()
    })
    // 不崩、仍渲染观看进度概览（已看 0 / 共 5）
    expect(screen.getByText(/已看 0 \/ 共 5/)).toBeVisible()
  })

  it('观看 tab 顶部渲染当前值卡（已看/未看/追看中）', async () => {
    useDefaultHandlers()
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('观看进度概览')).toBeVisible()
    })
    // 当前值卡标题三项（「追看中」唯一，「已看/未看」另有徽标同名故只查存在性）
    expect(screen.getByText('追看中')).toBeInTheDocument()
    expect(screen.getAllByText('已看').length).toBeGreaterThan(0)
    expect(screen.getAllByText('未看').length).toBeGreaterThan(0)
    // 追看中取自 continue-watching 列表长度（3）——定位到「追看中」所在卡内的大数
    const followCard = screen.getByText('追看中').closest('.mantine-Card-root') as HTMLElement
    expect(followCard).toBeTruthy()
    expect(followCard.textContent).toContain('3')
  })

  it('观看 tab 渲染观看活跃趋势折线卡', async () => {
    useDefaultHandlers()
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('观看活跃趋势')).toBeVisible()
    })
  })

  it('渲染续播位置热力 10 档', async () => {
    useDefaultHandlers()
    renderPage()
    await waitFor(() => {
      expect(screen.getByTestId('heat-cell-0')).toBeInTheDocument()
    })
    for (let i = 0; i < 10; i++) {
      expect(screen.getByTestId(`heat-cell-${i}`)).toBeInTheDocument()
    }
  })

  it('渲染各库 / 各格式分布与 Top 榜', async () => {
    useDefaultHandlers()
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('电影')).toBeVisible()
    })
    expect(screen.getByText('剧集')).toBeVisible()
    expect(screen.getByText('mp4')).toBeVisible()
    expect(screen.getByText('热门片.mp4')).toBeVisible()
    expect(screen.getByText('5 次')).toBeVisible()
  })

  it('热力方块与时间线条用品牌紫阶（FR-101 可视化精修）', async () => {
    useDefaultHandlers()
    renderPage()
    const cell0 = await screen.findByTestId('heat-cell-0')
    expect(cell0.style.background).toContain('--mantine-color-purple-6')
    expect(cell0.style.background).toContain('color-mix')
    const bar = screen.getByLabelText('2026-06-24 观看 3 个')
    const fill = bar.querySelector('div') as HTMLElement
    expect(fill.style.background).toContain('--mantine-color-purple-6')
  })

  it('所有进度条都有可访问名（FR-97：progressbar 无名修复）', async () => {
    useDefaultHandlers()
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('观看进度概览')).toBeVisible()
    })
    const bars = screen.getAllByRole('progressbar')
    expect(bars.length).toBeGreaterThan(0)
    for (const bar of bars) {
      expect(bar).toHaveAccessibleName()
    }
  })

  it('热力方块与时间线条用 role=img + 可访问名（FR-97：禁用 aria-label on div 修复）', async () => {
    useDefaultHandlers()
    renderPage()
    const cell0 = await screen.findByTestId('heat-cell-0')
    expect(cell0).toHaveAttribute('role', 'img')
    const bar = screen.getByLabelText('2026-06-24 观看 3 个')
    expect(bar).toHaveAttribute('role', 'img')
  })

  it('未看灰色徽标文字用更深 gray-7 以达对比度（FR-97）', async () => {
    useDefaultHandlers()
    const { container } = renderPage()
    await waitFor(() => {
      expect(screen.getByText('观看进度概览')).toBeVisible()
    })
    // 「未看」文案出现两处（MetricCard 卡标题 + 概览徽标）；仅校验徽标 label 被强制为更深 gray-7
    const badgeLabels = Array.from(container.querySelectorAll('.mantine-Badge-label')) as HTMLElement[]
    const unwatchedBadge = badgeLabels.find((el) => el.textContent === '未看')
    expect(unwatchedBadge).toBeTruthy()
    expect(unwatchedBadge!.style.color).toContain('--mantine-color-gray-7')
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
    expect(screen.getByRole('button', { name: /去添加媒体库/ })).toBeVisible()
    // 不渲染任何空图表卡：热力网格与 tab 都不出现
    expect(screen.queryByTestId('heat-cell-0')).toBeNull()
    expect(screen.queryByText('观看进度概览')).toBeNull()
    expect(screen.queryByRole('tab', { name: /观看/ })).toBeNull()
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
    server.use(
      http.get('*/api/library/stats', () => HttpResponse.json(sparseStats)),
      http.get('*/api/library/summary', () => HttpResponse.json(sampleSummary)),
      http.get('*/api/library/trends', () => HttpResponse.json(sampleTrends)),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('观看进度概览')).toBeVisible()
    })
    expect(screen.queryByText('续播位置热力')).toBeNull()
    expect(screen.queryByText('最近观看时间线')).toBeNull()
    expect(screen.queryByTestId('heat-cell-0')).toBeNull()
  })

  it('切到媒体 tab 渲染媒体当前值卡与增长曲线（FR-118）', async () => {
    useDefaultHandlers()
    const user = userEvent.setup()
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /媒体/ })).toBeInTheDocument()
    })
    await user.click(screen.getByRole('tab', { name: /媒体/ }))
    // 媒体 tab 当前值卡与图表标题
    await waitFor(() => {
      expect(screen.getByText('媒体总数')).toBeVisible()
    })
    expect(screen.getByText('媒体增长曲线')).toBeVisible()
    expect(screen.getByText('累计容量增长')).toBeVisible()
    expect(screen.getByText('累计时长增长')).toBeVisible()
    expect(screen.getByText('媒体构成')).toBeVisible()
  })

  it('媒体 tab 经 ?tab=media 直接进入并记忆', async () => {
    useDefaultHandlers()
    renderPage('/stats?tab=media')
    // 直接落 media tab：渲染媒体当前值卡，而非观看进度概览
    await waitFor(() => {
      expect(screen.getByText('媒体总数')).toBeVisible()
    })
    expect(screen.queryByText('观看进度概览')).toBeNull()
  })
})
