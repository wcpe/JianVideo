import { render, screen, waitFor, within } from '@testing-library/react'
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

  it('无库定位参数时以聚合虚拟根初始化并列出所有存储库（FR-66）', async () => {
    let requestedParentPath: string | null = null
    let requestedLibraryID: string | null = null
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const url = new URL(request.url)
        requestedParentPath = url.searchParams.get('parent_path')
        requestedLibraryID = url.searchParams.get('library_id')
        return HttpResponse.json({
          breadcrumbs: [{ name: '全部存储库', path: '__root__' }],
          directories: [
            { name: '电影库', path: 'D:\\Videos\\Movies', library_id: 1 },
            { name: '动漫库', path: 'D:\\Videos\\Anime', library_id: 2 },
          ],
          files: [],
        })
      }),
    )

    renderPage()

    // 初始请求走聚合虚拟根，不带 library_id
    await waitFor(() => {
      expect(requestedParentPath).toBe('__root__')
      expect(requestedLibraryID).toBeNull()
    })
    // 根层列出所有库
    expect(await screen.findByText('电影库')).toBeVisible()
    expect(await screen.findByText('动漫库')).toBeVisible()
  })

  it('根层点击某库下钻进入该库树，带上对应 library_id（FR-66）', async () => {
    const requests: { libraryID: string | null; parentPath: string | null }[] = []
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const url = new URL(request.url)
        const parentPath = url.searchParams.get('parent_path')
        requests.push({ libraryID: url.searchParams.get('library_id'), parentPath })
        if (parentPath === '__root__') {
          return HttpResponse.json({
            breadcrumbs: [{ name: '全部存储库', path: '__root__' }],
            directories: [{ name: '动漫库', path: 'D:\\Videos\\Anime', library_id: 5 }],
            files: [],
          })
        }
        return HttpResponse.json({
          breadcrumbs: [{ name: '全部存储库', path: '__root__' }, { name: '动漫库', path: 'D:\\Videos\\Anime' }],
          directories: [], files: [],
        })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    // 点击根层的库目录卡片
    await user.click(await screen.findByText('动漫库'))

    // 下钻请求带 library_id=5 与该库路径
    await waitFor(() => {
      expect(requests.some(r => r.libraryID === '5' && r.parentPath === 'D:\\Videos\\Anime')).toBe(true)
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

    // 带库定位参数直接进库（筛选需库上下文，FR-66 根层无库）
    renderPage(`/browse?library_id=1&path=${encodeURIComponent('D:\\Videos\\Movies')}`)
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

describe('BrowsePage 移动端筛选折叠（FR-86）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    server.use(
      http.get('*/api/library/paths', () => HttpResponse.json({
        items: [{ id: 1, path: 'D:\\Videos\\Movies', type: 'local', label: '电影', enabled: true, created_at: '2025-01-01T00:00:00Z' }],
      })),
      http.get('*/api/library/browse', () => HttpResponse.json({
        breadcrumbs: [{ name: '电影', path: 'D:\\Videos\\Movies' }], directories: [], files: [],
      })),
      http.get('*/api/library/media', () => HttpResponse.json({ items: [], total: 0, page: 1, page_size: 100 })),
    )
  })

  it('提供「筛选」抽屉触发器，搜索框常驻在抽屉之外', async () => {
    renderPage(`/browse?library_id=1&path=${encodeURIComponent('D:\\Videos\\Movies')}`)
    await screen.findByText('电影')

    expect(screen.getByPlaceholderText(/在当前目录下搜索/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '筛选' })).toBeInTheDocument()
    // 默认抽屉关闭，无弹层
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('点「筛选」展开抽屉后，抽屉内选择类型「图片」仍透传 type=image', async () => {
    let mediaType: string | null = null
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        mediaType = new URL(request.url).searchParams.get('type')
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 100 })
      }),
    )

    const user = userEvent.setup()
    renderPage(`/browse?library_id=1&path=${encodeURIComponent('D:\\Videos\\Movies')}`)
    await screen.findByText('电影')

    // 打开筛选抽屉，在抽屉内选择类型「图片」
    await user.click(screen.getByRole('button', { name: '筛选' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('radio', { name: '图片' }))

    await waitFor(() => expect(mediaType).toBe('image'), { timeout: 2000 })
  })
})
