import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse, delay } from 'msw'
import LibraryPage from './LibraryPage'
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

function renderLibraryPage(route = '/library') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[route]}>
        <LibraryPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('LibraryPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染媒体库管理标题', async () => {
    renderLibraryPage()
    await waitFor(() => {
      expect(screen.getByText('媒体库')).toBeVisible()
    })
  })

  it('显示添加路径输入框和按钮', async () => {
    renderLibraryPage()
    await waitFor(() => {
      expect(screen.getByPlaceholderText('输入目录路径，如 D:\\Videos')).toBeVisible()
      expect(screen.getByRole('button', { name: '添加' })).toBeVisible()
    })
  })

  it('支持外部路由指定默认时间轴 Tab', async () => {
    render(
      <MantineProvider>
        <MemoryRouter initialEntries={['/timeline']}>
          <LibraryPage initialTab="timeline" />
        </MemoryRouter>
      </MantineProvider>,
    )

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /时间轴/ })).toHaveAttribute('aria-selected', 'true')
    })
  })

  it('时间轴媒体列表请求显式带上 time_desc 排序', async () => {
    let requestedSort: string | null = null
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        requestedSort = new URL(request.url).searchParams.get('sort')
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 })
      }),
    )

    renderLibraryPage()

    await waitFor(() => {
      expect(requestedSort).toBe('time_desc')
    })
  })

  it('目录 Tab 初始化使用完整媒体库路径', async () => {
    let requestedParentPath: string | null = null
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const url = new URL(request.url)
        requestedParentPath = url.searchParams.get('parent_path')
        return HttpResponse.json({
          breadcrumbs: [{ name: '电影', path: 'D:\\Videos\\Movies' }],
          directories: [],
          files: [],
        })
      }),
    )

    renderLibraryPage('/library?tab=directory')

    await waitFor(() => {
      expect(requestedParentPath).toBe('D:\\Videos\\Movies')
    })
  })

  it('目录卡片浏览按钮切到目录 Tab 并浏览完整路径', async () => {
    const browseRequests: string[] = []
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path') || ''
        browseRequests.push(parentPath)
        return HttpResponse.json({
          breadcrumbs: [{ name: '电影', path: parentPath }],
          directories: [],
          files: [],
        })
      }),
    )

    const user = userEvent.setup()
    renderLibraryPage()

    const movieCard = await screen.findByText('电影')
    await user.click(within(movieCard.closest('.mantine-Card-root') as HTMLElement).getByRole('button', { name: '浏览' }))

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /文件目录/ })).toHaveAttribute('aria-selected', 'true')
      expect(browseRequests).toContain('D:\\Videos\\Movies')
    })
  })

  it('为指定目录追加自定义后缀时带上 library_id', async () => {
    let payload: { library_id?: number; extension?: string; type?: string } | null = null
    server.use(
      http.post('*/api/library/extensions', async ({ request }) => {
        payload = await request.json() as { library_id: number; extension: string; type: string }
        return new HttpResponse(null, { status: 201 })
      }),
    )

    const user = userEvent.setup()
    renderLibraryPage()

    const movieCard = (await screen.findByText('电影')).closest('.mantine-Card-root') as HTMLElement
    await user.type(within(movieCard).getByLabelText('电影 自定义后缀'), '.foo')
    await user.click(within(movieCard).getByRole('button', { name: '图片' }))
    await user.click(within(movieCard).getByRole('button', { name: '添加后缀' }))

    await waitFor(() => {
      expect(payload).toEqual({ library_id: 1, extension: '.foo', type: 'image' })
    })
  })

  it('扫描中显示可感知的不确定进度状态', async () => {
    server.use(
      http.post('*/api/library/scan/:id', async () => {
        await delay(100)
        return HttpResponse.json({ scanned: 1 })
      }),
    )

    const user = userEvent.setup()
    renderLibraryPage()

    await screen.findByText('电影')
    const scanButton = screen.getAllByRole('button', { name: '扫描' })[0]
    await user.click(scanButton)

    expect(screen.getByRole('status')).toHaveTextContent('正在扫描')
  })

  it('扫描当前浏览目录后刷新目录', async () => {
    let browseCount = 0
    server.use(
      http.get('*/api/library/browse', () => {
        browseCount += 1
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] })
      }),
      http.post('*/api/library/scan/:id', () => HttpResponse.json({ scanned: 1 })),
    )

    const user = userEvent.setup()
    renderLibraryPage('/library?tab=directory')

    await waitFor(() => {
      expect(browseCount).toBeGreaterThan(0)
    })

    await screen.findByText('电影')
    const scanButton = screen.getAllByRole('button', { name: '扫描' })[0]
    await user.click(scanButton)

    await waitFor(() => {
      expect(browseCount).toBeGreaterThan(1)
    })
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
    renderLibraryPage()

    await user.click(await screen.findByText('海报.jpg'))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('img', { name: '海报.jpg' })).toHaveAttribute('src', '/api/library/media/88/raw')
    expect(mockNavigate).not.toHaveBeenCalledWith('/play/88')
    expect(screen.queryByText('0:00')).not.toBeInTheDocument()
  })

  it('图片卡片渲染缩略图指向 /raw，视频卡片不渲染缩略图', async () => {
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

    renderLibraryPage()

    // 等列表渲染
    await screen.findByText('海报.png')
    await screen.findByText('星际穿越.mkv')

    const pngThumb = screen.getByRole('img', { name: '海报.png' })
    expect(pngThumb).toHaveAttribute('src', '/api/library/media/200/raw')

    // 视频卡片不应有缩略图（避免范围扩散到 Bug B）
    expect(screen.queryByRole('img', { name: '星际穿越.mkv' })).not.toBeInTheDocument()
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
    renderLibraryPage()

    await user.click(await screen.findByText('封面.foo'))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('img', { name: '封面.foo' })).toHaveAttribute('src', '/api/library/media/99/raw')
    expect(mockNavigate).not.toHaveBeenCalledWith('/play/99')
  })

  it('重复点击添加目录只提交一次', async () => {
    let createCount = 0
    server.use(
      http.post('*/api/library/paths', async ({ request }) => {
        createCount += 1
        await delay(100)
        const body = await request.json() as { path: string; type: string; label: string }
        return HttpResponse.json({
          id: 99,
          path: body.path,
          type: body.type,
          label: body.label || body.path,
          enabled: true,
          created_at: new Date().toISOString(),
        }, { status: 201 })
      }),
    )

    const user = userEvent.setup()
    renderLibraryPage()

    await user.type(await screen.findByPlaceholderText('输入目录路径，如 D:\\Videos'), 'D:\\Videos\\New')
    const addButton = screen.getByRole('button', { name: '添加' })
    await user.dblClick(addButton)

    await waitFor(() => {
      expect(createCount).toBe(1)
    })
  })

  it('目录加载错误在目录 Tab 内可见', async () => {
    server.use(
      http.get('*/api/library/browse', () => HttpResponse.json({ message: '目录无法访问' }, { status: 500 })),
    )

    renderLibraryPage('/library?tab=directory')

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /文件目录/ })).toHaveAttribute('aria-selected', 'true')
      expect(screen.getByText('目录无法访问')).toBeVisible()
    })
  })
})
