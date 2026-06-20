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

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/browse']}>
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
