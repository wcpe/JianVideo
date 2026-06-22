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

  // ─── 回收站清理（FR-26）──────────────────────────────

  it('有软删项时展示清理回收站按钮', async () => {
    server.use(
      http.get('*/api/library/recycle', () => HttpResponse.json({
        items: [makeMedia(3, '要清的.mkv')],
      })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /清理回收站/ })).toBeVisible()
    })
  })

  it('点击清理弹二次确认，确认后调用清理接口并清空列表', async () => {
    let cleaned = false
    server.use(
      http.get('*/api/library/recycle', () => HttpResponse.json({
        items: cleaned ? [] : [makeMedia(5, '待清理.mkv')],
      })),
      http.post('*/api/library/recycle/cleanup', () => {
        cleaned = true
        return HttpResponse.json({ moved: 1, failed: 0 })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /清理回收站/ }))
    // 二次确认弹窗里的确认按钮
    await user.click(await screen.findByRole('button', { name: '清理' }))

    await waitFor(() => {
      expect(cleaned).toBe(true)
    })
    await waitFor(() => {
      expect(screen.getByText(/回收站是空的/)).toBeVisible()
    })
  })

  it('未配置回收站路径（409）时提示去设置页配置', async () => {
    server.use(
      http.get('*/api/library/recycle', () => HttpResponse.json({
        items: [makeMedia(9, '没配路径.mkv')],
      })),
      http.post('*/api/library/recycle/cleanup', () =>
        HttpResponse.json({ code: 'RECYCLE_PATH_UNSET', message: '以下盘符未配置回收站路径: D' }, { status: 409 })),
    )

    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /清理回收站/ }))
    await user.click(await screen.findByRole('button', { name: '清理' }))

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalled()
    })
    const msgArg = mockNotificationShow.mock.calls.map(c => JSON.stringify(c[0])).join(' ')
    expect(msgArg).toMatch(/设置/)
  })
})
