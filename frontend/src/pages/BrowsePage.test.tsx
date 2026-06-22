import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import BrowsePage from './BrowsePage'
import { server } from '@/mocks/beforeAll'

const mockNavigate = vi.fn()

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: vi.fn(),
  },
}))

function renderPage(initialEntry = '/browse') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <BrowsePage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('BrowsePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('目录初始化使用完整媒体库路径', async () => {
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

    renderPage()

    await waitFor(() => {
      expect(requestedParentPath).toBe('D:\\Videos\\Movies')
    })
  })

  it('带库定位查询参数时使用指定库与路径初始化', async () => {
    let requestedLibraryID: string | null = null
    let requestedParentPath: string | null = null
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const url = new URL(request.url)
        requestedLibraryID = url.searchParams.get('library_id')
        requestedParentPath = url.searchParams.get('parent_path')
        return HttpResponse.json({
          breadcrumbs: [{ name: '动漫', path: 'D:\\Videos\\Anime' }],
          directories: [],
          files: [],
        })
      }),
    )

    renderPage(`/browse?library_id=3&path=${encodeURIComponent('D:\\Videos\\Anime')}`)

    await waitFor(() => {
      expect(requestedLibraryID).toBe('3')
      expect(requestedParentPath).toBe('D:\\Videos\\Anime')
    })
  })

  it('目录下搜索按当前路径查媒体接口，透传表达式与类型（FR-36，消费 FR-35）', async () => {
    let mediaPath: string | null = null
    let mediaSearch: string | null = null
    let mediaType: string | null = null
    server.use(
      http.get('*/api/library/paths', () => HttpResponse.json({
        items: [{ id: 1, path: 'D:\\Videos\\Movies', type: 'local', label: '电影', enabled: true, created_at: '2025-01-01T00:00:00Z' }],
      })),
      http.get('*/api/library/browse', () => HttpResponse.json({
        breadcrumbs: [{ name: '电影', path: 'D:\\Videos\\Movies' }], directories: [], files: [],
      })),
      http.get('*/api/library/media', ({ request }) => {
        const url = new URL(request.url)
        mediaPath = url.searchParams.get('path')
        mediaSearch = url.searchParams.get('search')
        mediaType = url.searchParams.get('type')
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 100 })
      }),
    )

    renderPage()
    // 等目录初始化（面包屑出现）
    await screen.findByText('电影')

    // 输入表达式搜索 → 防抖后按当前目录路径查媒体接口
    await userEvent.type(screen.getByPlaceholderText(/在当前目录下搜索/), 'ext:jpg 海报')
    await waitFor(() => {
      expect(mediaSearch).toBe('ext:jpg 海报')
      expect(mediaPath).toBe('D:\\Videos\\Movies')
    }, { timeout: 2000 })

    // 选择类型「图片」→ 透传 type=image
    await userEvent.click(screen.getByRole('radio', { name: '图片' }))
    await waitFor(() => expect(mediaType).toBe('image'), { timeout: 2000 })
  })

  it('目录加载错误可见', async () => {
    server.use(
      http.get('*/api/library/browse', () => HttpResponse.json({ message: '目录无法访问' }, { status: 500 })),
    )

    renderPage()

    await waitFor(() => {
      expect(screen.getByText('目录无法访问')).toBeVisible()
    })
  })
})
