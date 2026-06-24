import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import AlbumsPage from './AlbumsPage'
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
      <MemoryRouter initialEntries={['/albums']}>
        <AlbumsPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('AlbumsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染相册页标题与新建按钮', async () => {
    renderPage()
    await waitFor(() => {
      // 页面标题为 h2 标题；面包屑亦含「相册」文字，故按标题角色断言避免歧义（FR-95 面包屑引入）
      expect(screen.getByRole('heading', { name: '相册' })).toBeVisible()
      expect(screen.getByRole('button', { name: '新建相册' })).toBeVisible()
    })
  })

  it('列出已有相册', async () => {
    server.use(
      http.get('*/api/albums', () => HttpResponse.json({
        items: [
          { id: 1, name: '旅行合集', description: '', cover_media_id: 0, created_at: '2025-01-01T00:00:00Z', updated_at: '2025-01-01T00:00:00Z', item_count: 3 },
        ],
      })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('旅行合集')).toBeVisible()
    })
  })

  it('新建相册并提交名称', async () => {
    let payload: { name?: string; description?: string } | null = null
    server.use(
      http.post('*/api/albums', async ({ request }) => {
        payload = await request.json() as { name: string; description: string }
        return HttpResponse.json({
          id: 9, name: payload.name, description: payload.description || '',
          cover_media_id: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
        }, { status: 201 })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: '新建相册' }))
    const nameInput = await screen.findByLabelText(/相册名称/)
    await user.type(nameInput, '夏日')
    await user.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => {
      expect(payload).toEqual({ name: '夏日', description: '' })
    })
  })

  it('点击相册查看其内容（成员列表）', async () => {
    server.use(
      http.get('*/api/albums', () => HttpResponse.json({
        items: [{ id: 5, name: '我的合集', description: '', cover_media_id: 0, created_at: '2025-01-01T00:00:00Z', updated_at: '2025-01-01T00:00:00Z', item_count: 1 }],
      })),
      http.get('*/api/albums/5/items', () => HttpResponse.json({
        items: [{
          id: 1, library_id: 1, file_path: 'D:\\A\\星际穿越.mkv', file_name: '星际穿越.mkv',
          file_size: 100, format: 'mkv', video_codec: 'h264', audio_codec: 'aac',
          duration: 100, width: 1920, height: 1080, bitrate: 5000000, subtitle_tracks: '',
          added_at: '2025-01-01T00:00:00Z', modified_at: '2025-01-01T00:00:00Z',
        }],
      })),
    )

    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByText('我的合集'))

    await waitFor(() => {
      expect(screen.getByText('星际穿越.mkv')).toBeVisible()
    })
  })

  it('在相册内移出媒体成员', async () => {
    let removeCalled: { albumId?: string; mediaId?: string } | null = null
    server.use(
      http.get('*/api/albums', () => HttpResponse.json({
        items: [{ id: 5, name: '合集', description: '', cover_media_id: 0, created_at: '2025-01-01T00:00:00Z', updated_at: '2025-01-01T00:00:00Z', item_count: 1 }],
      })),
      http.get('*/api/albums/5/items', () => HttpResponse.json({
        items: [{
          id: 1, library_id: 1, file_path: 'D:\\A\\片子.mkv', file_name: '片子.mkv',
          file_size: 100, format: 'mkv', video_codec: 'h264', audio_codec: 'aac',
          duration: 100, width: 1920, height: 1080, bitrate: 5000000, subtitle_tracks: '',
          added_at: '2025-01-01T00:00:00Z', modified_at: '2025-01-01T00:00:00Z',
        }],
      })),
      http.delete('*/api/albums/:id/items/:mediaId', ({ params }) => {
        removeCalled = { albumId: String(params.id), mediaId: String(params.mediaId) }
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByText('合集'))
    const card = (await screen.findByText('片子.mkv')).closest('.mantine-Card-root') as HTMLElement
    await user.click(within(card).getByRole('button', { name: '移出' }))

    await waitFor(() => {
      expect(removeCalled).toEqual({ albumId: '5', mediaId: '1' })
    })
  })

  it('无相册时渲染统一空态（FR-98）', async () => {
    server.use(
      http.get('*/api/albums', () => HttpResponse.json({ items: [] })),
    )
    renderPage()
    // 空态标题 + 接入 EmptyState 容器
    expect(await screen.findByText('还没有相册')).toBeVisible()
    const empties = screen.getAllByTestId('empty-state')
    expect(empties.length).toBeGreaterThan(0)
  })

  it('加载相册失败时渲染插画+重试空态（FR-98）', async () => {
    server.use(
      http.get('*/api/albums', () => new HttpResponse(null, { status: 500 })),
    )
    renderPage()
    expect(await screen.findByText('加载相册失败')).toBeVisible()
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
  })

  it('首屏加载相册列表时渲染骨架屏（FR-98）', async () => {
    // 让 /api/albums 挂起不结算，保持 loading
    server.use(
      http.get('*/api/albums', () => new Promise(() => {})),
    )
    const { container } = renderPage()
    await waitFor(() => {
      expect(container.querySelector('.mantine-Skeleton-root')).toBeInTheDocument()
    })
  })

  it('有成员的相册卡显示成员首图作为封面（FR-100）', async () => {
    let itemsRequested = false
    server.use(
      http.get('*/api/albums', () => HttpResponse.json({
        items: [{ id: 3, name: '风景', description: '', cover_media_id: 0, created_at: '2025-01-01T00:00:00Z', updated_at: '2025-01-01T00:00:00Z', item_count: 2 }],
      })),
      http.get('*/api/albums/3/items', () => {
        itemsRequested = true
        return HttpResponse.json({
          items: [{
            id: 42, library_id: 1, file_path: 'D:\\A\\海.jpg', file_name: '海.jpg',
            file_size: 100, format: 'jpg', video_codec: '', audio_codec: '',
            duration: 0, width: 1920, height: 1080, bitrate: 0, subtitle_tracks: '',
            added_at: '2025-01-01T00:00:00Z', modified_at: '2025-01-01T00:00:00Z',
          }],
        })
      }),
    )

    const { container } = renderPage()
    await screen.findByText('风景')

    // 取成员封面端点被调用，且渲染了首个成员（id=42）的缩略图 <img>
    await waitFor(() => expect(itemsRequested).toBe(true))
    await waitFor(() => {
      const cover = container.querySelector('img[src*="/api/library/thumbnail/42"]')
      expect(cover).not.toBeNull()
    })
  })

  it('空相册卡显示封面占位插画、不请求成员（FR-100）', async () => {
    let itemsRequested = false
    server.use(
      http.get('*/api/albums', () => HttpResponse.json({
        items: [{ id: 8, name: '空合集', description: '', cover_media_id: 0, created_at: '2025-01-01T00:00:00Z', updated_at: '2025-01-01T00:00:00Z', item_count: 0 }],
      })),
      http.get('*/api/albums/8/items', () => {
        itemsRequested = true
        return HttpResponse.json({ items: [] })
      }),
    )

    renderPage()
    // 占位插画存在（aria-label 标识封面占位）
    expect(await screen.findByLabelText('空合集 封面占位')).toBeInTheDocument()
    // 空相册不发成员请求（避免无谓 N 次请求）
    expect(itemsRequested).toBe(false)
  })

  it('在相册内从媒体库加入媒体', async () => {
    let addPayload: { media_id?: number } | null = null
    server.use(
      http.get('*/api/albums', () => HttpResponse.json({
        items: [{ id: 7, name: '收纳', description: '', cover_media_id: 0, created_at: '2025-01-01T00:00:00Z', updated_at: '2025-01-01T00:00:00Z', item_count: 0 }],
      })),
      http.get('*/api/albums/7/items', () => HttpResponse.json({ items: [] })),
      http.post('*/api/albums/:id/items', async ({ request }) => {
        addPayload = await request.json() as { media_id: number }
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByText('收纳'))
    await user.click(await screen.findByRole('button', { name: '加入媒体' }))

    // 媒体选择弹窗中点击某个媒体（来自默认 mock 媒体库）
    const pickItem = await screen.findByText('星际穿越.mkv')
    await user.click(pickItem)

    await waitFor(() => {
      expect(addPayload).toEqual({ media_id: 1 })
    })
  })
})
