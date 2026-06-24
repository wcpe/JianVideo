import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse, delay } from 'msw'
import TimelinePage from './TimelinePage'
import { server } from '@/mocks/beforeAll'

const mockNavigate = vi.fn()
const mockNotificationShow = vi.fn()

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}))

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/']}>
        <TimelinePage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('TimelinePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('时间轴媒体列表按媒体时间组织（sort=media_time，FR-32）', async () => {
    let requestedSort: string | null = null
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        requestedSort = new URL(request.url).searchParams.get('sort')
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 })
      }),
    )

    renderPage()

    await waitFor(() => {
      expect(requestedSort).toBe('media_time')
    })
  })

  it('按日期分组渲染日期文本，图片与视频卡片均渲染缩略图', async () => {
    server.use(
      http.get('*/api/library/media', () => HttpResponse.json({
        items: [
          {
            id: 200, library_id: 1,
            file_path: 'D:\\Videos\\Movies\\海报.png',
            file_name: '海报.png', file_size: 1200000, format: 'png',
            video_codec: '', audio_codec: '', duration: 0,
            width: 1200, height: 800, bitrate: 0, subtitle_tracks: '',
            added_at: '2025-01-09T12:00:00Z', modified_at: '2025-01-09T12:00:00Z',
          },
          {
            id: 201, library_id: 1,
            file_path: 'D:\\Videos\\Movies\\星际穿越.mkv',
            file_name: '星际穿越.mkv', file_size: 15_000_000_000, format: 'mkv',
            video_codec: 'hevc', audio_codec: 'aac', duration: 10020,
            width: 3840, height: 2160, bitrate: 12000000, subtitle_tracks: '',
            added_at: '2025-01-01T12:00:00Z', modified_at: '2025-01-01T12:00:00Z',
          },
        ],
        total: 2, page: 1, page_size: 20,
      })),
    )

    renderPage()

    await screen.findByText('海报.png')
    await screen.findByText('星际穿越.mkv')

    // 两个不同日期分别渲染日期轴文本：年份 2025 与月日 01-09 / 01-01
    expect(screen.getAllByText('2025').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('01-09')).toBeInTheDocument()
    expect(screen.getByText('01-01')).toBeInTheDocument()

    // 图片卡片渲染缩略图指向缩略图 URL（多尺寸：带 size 参数，FR-81）
    const pngThumb = screen.getByRole('img', { name: '海报.png' })
    expect(pngThumb.getAttribute('src')).toContain('/api/library/thumbnail/200')

    // 视频卡片现在同样渲染缩略图
    const mkvThumb = screen.getByRole('img', { name: '星际穿越.mkv' })
    expect(mkvThumb.getAttribute('src')).toContain('/api/library/thumbnail/201')
  })

  it('点击刷新按钮重载首页数据（FR-67）', async () => {
    let requestCount = 0
    server.use(
      http.get('*/api/library/media', () => {
        requestCount += 1
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    // 首屏会发起一次请求
    await waitFor(() => {
      expect(requestCount).toBe(1)
    })

    // 点击刷新按钮应再次发起首页请求
    await user.click(screen.getByRole('button', { name: '刷新' }))

    await waitFor(() => {
      expect(requestCount).toBe(2)
    })
  })

  it('切换缩放粒度（日/月/年）改变日期轴分组（FR-32 缺陷复现）', async () => {
    // 两条同年同月不同日的媒体，带 media_time：
    // 日粒度 → 两组（07-01 / 07-20）；月粒度 → 一组（07）；年粒度 → 一组（2023）
    server.use(
      http.get('*/api/library/media', () => HttpResponse.json({
        items: [
          {
            id: 301, library_id: 1,
            file_path: 'D:\\Photos\\a.jpg', file_name: 'a.jpg',
            file_size: 1000, format: 'jpg',
            video_codec: '', audio_codec: '', duration: 0,
            width: 100, height: 100, bitrate: 0, subtitle_tracks: '',
            added_at: '2020-01-01T00:00:00Z', modified_at: '2020-01-01T00:00:00Z',
            media_time: '2023-07-20T10:00:00Z',
          },
          {
            id: 302, library_id: 1,
            file_path: 'D:\\Photos\\b.jpg', file_name: 'b.jpg',
            file_size: 1000, format: 'jpg',
            video_codec: '', audio_codec: '', duration: 0,
            width: 100, height: 100, bitrate: 0, subtitle_tracks: '',
            added_at: '2020-01-01T00:00:00Z', modified_at: '2020-01-01T00:00:00Z',
            media_time: '2023-07-01T08:00:00Z',
          },
        ],
        total: 2, page: 1, page_size: 20,
      })),
    )

    const user = userEvent.setup()
    renderPage()

    // 默认日粒度：两个不同日的日期轴主标签 07-20 / 07-01
    await screen.findByText('07-20')
    expect(screen.getByText('07-01')).toBeInTheDocument()

    // 切到月粒度：合并为单组，主标签为月份 07，且不再出现日级标签
    await user.click(screen.getByRole('radio', { name: '月' }))
    await screen.findByText('07')
    expect(screen.queryByText('07-20')).not.toBeInTheDocument()
    expect(screen.queryByText('07-01')).not.toBeInTheDocument()

    // 切到年粒度：主标签为年份 2023，且不再出现月级标签 07
    await user.click(screen.getByRole('radio', { name: '年' }))
    await screen.findByText('2023')
    expect(screen.queryByText('07')).not.toBeInTheDocument()
  })

  it('图片点击打开预览弹窗且不跳转播放页', async () => {
    server.use(
      http.get('*/api/library/media', () => HttpResponse.json({
        items: [{
          id: 88,
          library_id: 1,
          file_path: 'D:\\Videos\\Movies\\海报.jpg',
          file_name: '海报.jpg',
          file_size: 1200000,
          format: 'jpg',
          video_codec: '',
          audio_codec: '',
          duration: 0,
          width: 1200,
          height: 800,
          bitrate: 0,
          subtitle_tracks: '',
          added_at: '2025-01-09T12:00:00Z',
          modified_at: '2025-01-09T12:00:00Z',
        }],
        total: 1,
        page: 1,
        page_size: 20,
      })),
    )

    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByText('海报.jpg'))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('img', { name: '海报.jpg' })).toHaveAttribute('src', '/api/library/media/88/raw')
    expect(mockNavigate).not.toHaveBeenCalledWith('/play/88')
    expect(screen.queryByText('0:00')).not.toBeInTheDocument()
  })

  it('自定义图片后缀扫描出的文件点击预览而非跳转播放', async () => {
    server.use(
      http.get('*/api/library/paths', () => HttpResponse.json({
        items: [{ id: 1, path: 'D:\\Videos\\Movies', type: 'local', label: '电影', enabled: true, created_at: '2025-01-09T12:00:00Z' }],
      })),
      http.get('*/api/library/extensions', () => HttpResponse.json({
        items: [{ id: 1, library_id: 1, extension: 'foo', type: 'image', is_builtin: 0, created_at: '2025-01-09T12:00:00Z' }],
      })),
      http.get('*/api/library/media', async () => {
        await delay(50)
        return HttpResponse.json({
          items: [{
            id: 99,
            library_id: 1,
            file_path: 'D:\\Videos\\Movies\\封面.foo',
            file_name: '封面.foo',
            file_size: 1200000,
            format: 'foo',
            video_codec: '',
            audio_codec: '',
            duration: 0,
            width: 1200,
            height: 800,
            bitrate: 0,
            subtitle_tracks: '',
            added_at: '2025-01-09T12:00:00Z',
            modified_at: '2025-01-09T12:00:00Z',
          }],
          total: 1,
          page: 1,
          page_size: 20,
        })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByText('封面.foo'))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('img', { name: '封面.foo' })).toHaveAttribute('src', '/api/library/media/99/raw')
    expect(mockNavigate).not.toHaveBeenCalledWith('/play/99')
  })
})
