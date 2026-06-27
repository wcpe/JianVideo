import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import OverviewPage from './OverviewPage'
import { server } from '@/mocks/beforeAll'
import type { LibrarySummary, WatchStats } from '@/types'

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

// 一组多库示例聚合（FR-117）：覆盖视频/图片拆分、各库分布、总大小/时长
const sampleSummary: LibrarySummary = {
  total: 100,
  video_count: 60,
  image_count: 40,
  total_size: 5_000_000_000,
  total_duration: 36000,
  library_count: 2,
  by_library: [
    { library_id: 1, label: '电影', media_count: 70, video_count: 60, image_count: 10, total_size: 4_000_000_000, total_duration: 36000 },
    { library_id: 2, label: '照片', media_count: 30, video_count: 0, image_count: 30, total_size: 1_000_000_000, total_duration: 0 },
  ],
}

const sampleWatch: WatchStats = {
  total: 100,
  watched: 25,
  unwatched: 75,
  recent_timeline: [],
  position_heatmap: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
  by_library: [],
  by_format: [],
  top_viewed: [],
}

// 注册概览页所需、默认 handler 未覆盖的四个端点；其余（system/info、albums、scan/tasks、continue-watching）走默认 handler。
function useOverviewHandlers(opts?: {
  summary?: LibrarySummary
  watch?: WatchStats
  issueCount?: number
}) {
  const summary = opts?.summary ?? sampleSummary
  const watch = opts?.watch ?? sampleWatch
  const issueCount = opts?.issueCount ?? 0
  server.use(
    http.get('*/api/library/summary', () => HttpResponse.json(summary)),
    http.get('*/api/library/stats', () => HttpResponse.json(watch)),
    http.get('*/api/library/health/status', () => HttpResponse.json({
      status: 'completed', total: 0, checked: 0, issue_count: issueCount, error: '', started_at: '', completed_at: '',
    })),
    http.get('*/api/transcode/tasks', () => HttpResponse.json({ tasks: [] })),
    // 继续观看返回空，使该块不渲染，避免与 KPI 数字干扰断言
    http.get('*/api/library/continue-watching', () => HttpResponse.json({ items: [] })),
  )
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/']}>
        <OverviewPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('OverviewPage（FR-117 首页概览看板）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染标题与各分区小标题', async () => {
    useOverviewHandlers()
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('概览')).toBeVisible()
    })
    // 六个分区中的关键标题均渲染
    expect(screen.getByText('媒体总数')).toBeVisible()
    expect(screen.getByText('媒体构成')).toBeVisible()
    expect(screen.getByText('各库分布')).toBeVisible()
    expect(screen.getByText('观看概览')).toBeVisible()
    expect(screen.getByText('系统状态')).toBeVisible()
    expect(screen.getByText('任务队列')).toBeVisible()
  })

  it('KPI 大数卡展示媒体总数与视频/图片副标', async () => {
    useOverviewHandlers()
    renderPage()

    // 媒体总数主数值
    await waitFor(() => {
      expect(screen.getByText('100')).toBeVisible()
    })
    // 副标含视频/图片拆分
    expect(screen.getByText(/视频 60 · 图片 40/)).toBeVisible()
    // 媒体库数与时长（小时：36000s → 10h）
    expect(screen.getByText('视频总时长（小时）')).toBeVisible()
    expect(screen.getByText('10')).toBeVisible()
  })

  it('各库分布渲染各库名与媒体数', async () => {
    useOverviewHandlers()
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('电影')).toBeVisible()
    })
    expect(screen.getByText('照片')).toBeVisible()
    // 各库 media_count
    expect(screen.getByText('70')).toBeVisible()
    expect(screen.getByText('30')).toBeVisible()
  })

  it('观看概览含已看占比环形与「查看全部」跳 /stats 链接', async () => {
    useOverviewHandlers()
    renderPage()

    // 25/100 → 25% 环形（role=img 可访问名；watch stats 异步加载后更新）
    await waitFor(() => {
      expect(screen.getByRole('img', { name: '已看占比 25%' })).toBeInTheDocument()
    })
    // 「查看全部 ›」为跳 /stats 的链接（静态渲染）
    const link = screen.getByRole('link', { name: /查看全部/ })
    expect(link).toHaveAttribute('href', '/stats')
  })

  it('系统状态展示 FFmpeg 可用与巡检问题数（复用 system/info 与 health/status）', async () => {
    useOverviewHandlers({ issueCount: 3 })
    renderPage()

    // 默认 system/info handler：ffmpeg.available=true（异步加载，需等待徽标出现）
    await waitFor(() => {
      expect(screen.getByText('可用')).toBeVisible()
    })
    // 巡检问题数徽标（health/status 异步）
    await waitFor(() => {
      expect(screen.getByText('3 个')).toBeVisible()
    })
  })

  it('任务队列展示转码三档计数（前端聚合 pending/running/completed）', async () => {
    server.use(
      http.get('*/api/library/summary', () => HttpResponse.json(sampleSummary)),
      http.get('*/api/library/stats', () => HttpResponse.json(sampleWatch)),
      http.get('*/api/library/health/status', () => HttpResponse.json({
        status: 'completed', total: 0, checked: 0, issue_count: 0, error: '', started_at: '', completed_at: '',
      })),
      http.get('*/api/library/continue-watching', () => HttpResponse.json({ items: [] })),
      http.get('*/api/transcode/tasks', () => HttpResponse.json({
        tasks: [
          { id: 1, media_id: 1, preset_id: 1, codec: 'h264', width: 0, height: 0, status: 'pending', error: '', created_at: '' },
          { id: 2, media_id: 2, preset_id: 1, codec: 'h264', width: 0, height: 0, status: 'running', error: '', created_at: '' },
          { id: 3, media_id: 3, preset_id: 1, codec: 'h264', width: 0, height: 0, status: 'completed', error: '', created_at: '' },
          { id: 4, media_id: 4, preset_id: 1, codec: 'h264', width: 0, height: 0, status: 'completed', error: '', created_at: '' },
        ],
      })),
    )
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('转码任务')).toBeVisible()
    })
    // 待处理 1 / 进行中 1 / 已完成 2
    expect(screen.getByText('待处理')).toBeVisible()
    expect(screen.getByText('进行中')).toBeVisible()
    expect(screen.getByText('已完成')).toBeVisible()
  })

  it('空库（summary.total=0）各卡渲染零值而不报错（AC-21）', async () => {
    const emptySummary: LibrarySummary = {
      total: 0, video_count: 0, image_count: 0, total_size: 0, total_duration: 0, library_count: 0, by_library: [],
    }
    const emptyWatch: WatchStats = {
      total: 0, watched: 0, unwatched: 0,
      recent_timeline: [], position_heatmap: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
      by_library: [], by_format: [], top_viewed: [],
    }
    useOverviewHandlers({ summary: emptySummary, watch: emptyWatch })
    renderPage()

    // 标题与分区仍渲染（不整页崩）
    await waitFor(() => {
      expect(screen.getByText('概览')).toBeVisible()
    })
    expect(screen.getByText('媒体总数')).toBeVisible()
    // 各库分布空态占位
    expect(screen.getByText('暂无媒体库数据。')).toBeVisible()
    // 观看占比环形为 0%
    expect(screen.getByRole('img', { name: '已看占比 0%' })).toBeInTheDocument()
    // 副标 视频 0 · 图片 0
    expect(screen.getByText(/视频 0 · 图片 0/)).toBeVisible()
  })

  it('summary 加载失败时各卡降级为零值/占位，不整页崩', async () => {
    server.use(
      http.get('*/api/library/summary', () => HttpResponse.json({ message: '炸了' }, { status: 500 })),
      http.get('*/api/library/stats', () => HttpResponse.json(sampleWatch)),
      http.get('*/api/library/health/status', () => HttpResponse.json({
        status: 'completed', total: 0, checked: 0, issue_count: 0, error: '', started_at: '', completed_at: '',
      })),
      http.get('*/api/transcode/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('*/api/library/continue-watching', () => HttpResponse.json({ items: [] })),
    )
    renderPage()

    // 即便 summary 500，页面骨架与分区仍在
    await waitFor(() => {
      expect(screen.getByText('概览')).toBeVisible()
    })
    expect(screen.getByText('媒体构成')).toBeVisible()
    expect(screen.getByText('暂无媒体库数据。')).toBeVisible()
  })
})
