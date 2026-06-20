import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse, delay } from 'msw'
import LibraryManagerPage from './LibraryManagerPage'
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
      <MemoryRouter initialEntries={['/library-manager']}>
        <LibraryManagerPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('LibraryManagerPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染存储库管理标题', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('存储库管理')).toBeVisible()
    })
  })

  it('显示添加路径输入框和按钮', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByPlaceholderText('输入目录路径，如 D:\\Videos')).toBeVisible()
      expect(screen.getByRole('button', { name: '添加' })).toBeVisible()
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
    renderPage()

    const movieCard = (await screen.findByText('电影')).closest('.mantine-Card-root') as HTMLElement
    await user.type(within(movieCard).getByLabelText('电影 自定义后缀'), '.foo')
    await user.click(within(movieCard).getByRole('button', { name: '图片' }))
    await user.click(within(movieCard).getByRole('button', { name: '添加后缀' }))

    await waitFor(() => {
      expect(payload).toEqual({ library_id: 1, extension: '.foo', type: 'image' })
    })
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
    renderPage()

    await user.type(await screen.findByPlaceholderText('输入目录路径，如 D:\\Videos'), 'D:\\Videos\\New')
    const addButton = screen.getByRole('button', { name: '添加' })
    await user.dblClick(addButton)

    await waitFor(() => {
      expect(createCount).toBe(1)
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
    renderPage()

    await screen.findByText('电影')
    const scanButton = screen.getAllByRole('button', { name: '扫描' })[0]
    await user.click(scanButton)

    expect(screen.getByRole('status')).toHaveTextContent('正在扫描')
  })

  it('重命名媒体文件成功并提交新文件名', async () => {
    let payload: { new_name?: string } | null = null
    server.use(
      http.get('*/api/library/media', () => HttpResponse.json({
        items: [{
          id: 77,
          library_id: 1,
          file_path: 'D:\\Videos\\Movies\\旧名字.mkv',
          file_name: '旧名字.mkv',
          file_size: 5_000_000_000,
          format: 'mkv',
          video_codec: 'h264',
          audio_codec: 'aac',
          duration: 3600,
          width: 1920,
          height: 1080,
          bitrate: 5000000,
          subtitle_tracks: '',
          added_at: '2025-01-09T12:00:00Z',
          modified_at: '2025-01-09T12:00:00Z',
        }],
        total: 1,
        page: 1,
        page_size: 20,
      })),
      http.put('*/api/library/media/:id/rename', async ({ request, params }) => {
        payload = await request.json() as { new_name: string }
        return HttpResponse.json({
          id: Number(params.id),
          library_id: 1,
          file_path: 'D:\\Videos\\Movies\\新名字.mkv',
          file_name: payload.new_name,
          file_size: 5_000_000_000,
          format: 'mkv',
          video_codec: 'h264',
          audio_codec: 'aac',
          duration: 3600,
          width: 1920,
          height: 1080,
          bitrate: 5000000,
          subtitle_tracks: '',
          added_at: '2025-01-09T12:00:00Z',
          modified_at: '2025-01-09T12:00:00Z',
        })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    const card = (await screen.findByText('旧名字.mkv')).closest('.mantine-Card-root') as HTMLElement
    await user.click(within(card).getByRole('button', { name: '重命名' }))

    const input = await screen.findByLabelText('新文件名')
    await user.clear(input)
    await user.type(input, '新名字.mkv')
    await user.click(screen.getByRole('button', { name: '确认' }))

    await waitFor(() => {
      expect(payload).toEqual({ new_name: '新名字.mkv' })
    })
  })
})
