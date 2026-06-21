import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import RecyclePage from './RecyclePage'
import { server } from '@/mocks/beforeAll'

const mockNotificationShow = vi.fn()

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}))

function makeMedia(id: number, name: string) {
  return {
    id, library_id: 1, file_path: `D:\\A\\${name}`, file_name: name,
    file_size: 100, format: 'mkv', video_codec: 'h264', audio_codec: 'aac',
    duration: 100, width: 1920, height: 1080, bitrate: 5000000, subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z', modified_at: '2025-01-01T00:00:00Z',
  }
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/recycle']}>
        <RecyclePage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('RecyclePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染回收站页标题', async () => {
    server.use(
      http.get('*/api/library/recycle', () => HttpResponse.json({ items: [] })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('回收站')).toBeVisible()
    })
  })

  it('空回收站显示提示文案', async () => {
    server.use(
      http.get('*/api/library/recycle', () => HttpResponse.json({ items: [] })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/回收站是空的/)).toBeVisible()
    })
  })

  it('列出已软删的媒体', async () => {
    server.use(
      http.get('*/api/library/recycle', () => HttpResponse.json({
        items: [makeMedia(1, '误删的片子.mkv')],
      })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('误删的片子.mkv')).toBeVisible()
    })
  })

  it('点击还原调用还原接口并从列表移除', async () => {
    let restoredID: string | null = null
    server.use(
      http.get('*/api/library/recycle', () => HttpResponse.json({
        items: [makeMedia(7, '待还原.mkv')],
      })),
      http.post('*/api/library/media/:id/restore', ({ params }) => {
        restoredID = String(params.id)
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    const card = (await screen.findByText('待还原.mkv')).closest('.mantine-Card-root') as HTMLElement
    await user.click(within(card).getByRole('button', { name: '还原' }))

    await waitFor(() => {
      expect(restoredID).toBe('7')
    })
  })
})
