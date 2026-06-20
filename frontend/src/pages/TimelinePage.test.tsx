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

  it('时间轴媒体列表请求显式带上 time_desc 排序', async () => {
    let requestedSort: string | null = null
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        requestedSort = new URL(request.url).searchParams.get('sort')
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 })
      }),
    )

    renderPage()

    await waitFor(() => {
      expect(requestedSort).toBe('time_desc')
    })
  })

  it('图片卡片渲染缩略图指向缩略图 URL，视频卡片不渲染缩略图', async () => {
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

    const pngThumb = screen.getByRole('img', { name: '海报.png' })
    expect(pngThumb).toHaveAttribute('src', '/api/library/thumbnail/200')

    // 视频卡片不应有缩略图
    expect(screen.queryByRole('img', { name: '星际穿越.mkv' })).not.toBeInTheDocument()
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
