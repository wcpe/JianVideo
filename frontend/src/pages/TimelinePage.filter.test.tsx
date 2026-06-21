import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import TimelinePage from './TimelinePage'
import { server } from '@/mocks/beforeAll'

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => vi.fn() }
})

vi.mock('@mantine/notifications', () => ({
  notifications: { show: vi.fn() },
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

describe('TimelinePage 收藏与标签筛选（FR-41）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('开启「仅收藏」后媒体请求带上 favorite=true', async () => {
    const favoriteParams: (string | null)[] = []
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        favoriteParams.push(new URL(request.url).searchParams.get('favorite'))
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 })
      }),
      http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })),
    )

    renderPage()
    // 首屏请求 favorite 为空
    await waitFor(() => expect(favoriteParams.length).toBeGreaterThan(0))

    const toggle = await screen.findByRole('switch', { name: '仅收藏' })
    await userEvent.click(toggle)

    await waitFor(() => {
      expect(favoriteParams).toContain('true')
    })
  })

  it('选择标签后媒体请求带上 tag_id', async () => {
    const tagParams: (string | null)[] = []
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        tagParams.push(new URL(request.url).searchParams.get('tag_id'))
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 })
      }),
      http.get('*/api/library/tags', () => HttpResponse.json({
        items: [{ id: 7, name: '精选', created_at: '2025-01-01T00:00:00Z' }],
      })),
    )

    renderPage()
    await waitFor(() => expect(tagParams.length).toBeGreaterThan(0))

    // 在原生下拉中选择「精选」
    const select = await screen.findByRole('combobox', { name: '标签筛选' })
    await waitFor(() => expect(screen.getByRole('option', { name: '精选' })).toBeInTheDocument())
    await userEvent.selectOptions(select, '7')

    await waitFor(() => {
      expect(tagParams).toContain('7')
    })
  })
})
