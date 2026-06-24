import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import DuplicatesPage from './DuplicatesPage'
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
    file_size: 100, format: 'jpg', video_codec: '', audio_codec: '',
    duration: 0, width: 1920, height: 1080, bitrate: 0, subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z', modified_at: '2025-01-01T00:00:00Z',
  }
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/duplicates']}>
        <DuplicatesPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('DuplicatesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染页面标题', async () => {
    server.use(http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })))
    renderPage()
    await waitFor(() => expect(screen.getByText('重复项')).toBeVisible())
  })

  it('无重复组时显示空态提示', async () => {
    server.use(http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })))
    renderPage()
    await waitFor(() => expect(screen.getByText(/没有发现重复/)).toBeVisible())
  })

  it('空态展示引导文案与可点扫描 CTA', async () => {
    let scanned = false
    server.use(
      http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })),
      http.post('*/api/library/duplicates/scan', () => {
        scanned = true
        return HttpResponse.json({ computed: 0 })
      }),
    )
    const user = userEvent.setup()
    renderPage()
    // 空态组件渲染（标题文案）
    expect(await screen.findByText('没有发现重复项')).toBeVisible()
    // 点击空态内的扫描 CTA（与顶栏同名，取空态卡片内的）
    const emptyState = screen.getByTestId('empty-state')
    await user.click(within(emptyState).getByRole('button', { name: /扫描重复项/ }))
    await waitFor(() => expect(scanned).toBe(true))
  })

  it('列出重复组成员', async () => {
    server.use(
      http.get('*/api/library/duplicates', () => HttpResponse.json({
        groups: [[makeMedia(1, '重复A.jpg'), makeMedia(2, '重复B.jpg')]],
      })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('重复A.jpg')).toBeVisible()
      expect(screen.getByText('重复B.jpg')).toBeVisible()
    })
  })

  it('点击扫描重复项调用扫描接口并刷新', async () => {
    let scanned = false
    server.use(
      http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })),
      http.post('*/api/library/duplicates/scan', () => {
        scanned = true
        return HttpResponse.json({ computed: 0 })
      }),
    )
    const user = userEvent.setup()
    renderPage()
    // 顶栏与空态 CTA 同名，点击顶栏（首个）即可触发
    const buttons = await screen.findAllByRole('button', { name: /扫描重复项/ })
    await user.click(buttons[0])
    await waitFor(() => expect(scanned).toBe(true))
  })

  it('选中成员后批量删除调用批量软删端点并刷新', async () => {
    let deletedIDs: number[] = []
    let listCalls = 0
    server.use(
      http.get('*/api/library/duplicates', () => {
        listCalls++
        // 第一次返回一组两项；删除后第二次返回空（已清理）
        if (listCalls === 1) {
          return HttpResponse.json({ groups: [[makeMedia(11, '保留.jpg'), makeMedia(12, '多余.jpg')]] })
        }
        return HttpResponse.json({ groups: [] })
      }),
      http.post('*/api/library/media/batch-delete', async ({ request }) => {
        const body = await request.json() as { ids: number[] }
        deletedIDs = body.ids
        return HttpResponse.json({ deleted: body.ids.length })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    // 勾选「多余.jpg」对应的复选框
    const card = (await screen.findByText('多余.jpg')).closest('.mantine-Card-root') as HTMLElement
    await user.click(within(card).getByRole('checkbox'))

    // 点击「删除选中项」
    await user.click(screen.getByRole('button', { name: /删除选中项/ }))

    await waitFor(() => expect(deletedIDs).toEqual([12]))
    // 删除后列表刷新为空态
    await waitFor(() => expect(screen.getByText(/没有发现重复/)).toBeVisible())
  })

  it('媒体行用统一 MediaRow 渲染（缩略图 + 勾选 + 文件名，FR-101）', async () => {
    server.use(
      http.get('*/api/library/duplicates', () => HttpResponse.json({
        groups: [[makeMedia(1, '统一行A.jpg'), makeMedia(2, '统一行B.jpg')]],
      })),
    )
    renderPage()
    const card = (await screen.findByText('统一行A.jpg')).closest('.mantine-Card-root') as HTMLElement
    // MediaRow 复用 MediaThumbnail：行内含缩略图 img
    expect(within(card).getByRole('img', { name: '统一行A.jpg' })).toBeInTheDocument()
    // 行内含勾选框
    expect(within(card).getByRole('checkbox', { name: '选择 统一行A.jpg' })).toBeInTheDocument()
  })

  it('未选中任何项时删除按钮禁用', async () => {
    server.use(
      http.get('*/api/library/duplicates', () => HttpResponse.json({
        groups: [[makeMedia(1, 'a.jpg'), makeMedia(2, 'b.jpg')]],
      })),
    )
    renderPage()
    const btn = await screen.findByRole('button', { name: /删除选中项/ })
    expect(btn).toBeDisabled()
  })
})
