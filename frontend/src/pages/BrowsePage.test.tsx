import { render, screen, waitFor } from '@testing-library/react'
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
