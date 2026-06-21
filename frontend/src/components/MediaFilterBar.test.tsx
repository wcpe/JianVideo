import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import MediaFilterBar from './MediaFilterBar'
import { server } from '@/mocks/beforeAll'

vi.mock('@mantine/notifications', () => ({
  notifications: { show: vi.fn() },
}))

function renderBar(props: Partial<React.ComponentProps<typeof MediaFilterBar>> = {}) {
  return render(
    <MantineProvider>
      <MediaFilterBar
        favorite={false}
        onFavoriteChange={() => {}}
        tagId={0}
        onTagIdChange={() => {}}
        {...props}
      />
    </MantineProvider>,
  )
}

describe('MediaFilterBar 标签管理（FR-41）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    server.use(http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })))
  })

  it('切换「仅收藏」开关回调', async () => {
    const onFavoriteChange = vi.fn()
    renderBar({ onFavoriteChange })
    await userEvent.click(screen.getByRole('switch', { name: '仅收藏' }))
    expect(onFavoriteChange).toHaveBeenCalledWith(true)
  })

  it('新建标签后调用接口并按新标签筛选', async () => {
    let created: string | null = null
    server.use(
      http.post('*/api/library/tags', async ({ request }) => {
        const body = await request.json() as { name: string }
        created = body.name
        return HttpResponse.json({ id: 42, name: body.name, created_at: '2025-01-01T00:00:00Z' }, { status: 201 })
      }),
    )
    const onTagIdChange = vi.fn()
    renderBar({ onTagIdChange })

    await userEvent.click(screen.getByRole('button', { name: '新建标签' }))
    await userEvent.type(await screen.findByRole('textbox', { name: '标签名' }), '风景')
    await userEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(created).toBe('风景'))
    await waitFor(() => expect(onTagIdChange).toHaveBeenCalledWith(42))
  })
})
