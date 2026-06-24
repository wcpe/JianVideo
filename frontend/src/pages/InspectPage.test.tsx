import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import InspectPage from './InspectPage'
import { server } from '@/mocks/beforeAll'

const mockNotificationShow = vi.fn()

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}))

const idleStatus = { status: 'idle', total: 0, checked: 0, issue_count: 0, error: '', started_at: '', completed_at: '' }
const completedStatus = { status: 'completed', total: 3, checked: 3, issue_count: 2, error: '', started_at: '', completed_at: '2025-01-01T00:00:00Z' }

function makeIssue(id: number, media_id: number, issue_type: string, file_name: string, detail = '') {
  return { id, media_id, issue_type, detail, checked_at: '2025-01-01T00:00:00Z', file_name, file_path: `D:\\A\\${file_name}`, library_id: 1 }
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/inspect']}>
        <InspectPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('InspectPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染问题媒体页标题与开始巡检按钮', async () => {
    server.use(
      http.get('*/api/library/health/status', () => HttpResponse.json(idleStatus)),
      http.get('*/api/library/health/issues', () => HttpResponse.json({ items: [] })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('问题媒体')).toBeVisible()
    })
    // 顶栏与空态 CTA 同名「开始巡检」，至少有一个可见
    expect(screen.getAllByRole('button', { name: /开始巡检/ }).length).toBeGreaterThan(0)
  })

  it('无问题时展示提示文案', async () => {
    server.use(
      http.get('*/api/library/health/status', () => HttpResponse.json(completedStatus)),
      http.get('*/api/library/health/issues', () => HttpResponse.json({ items: [] })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/未发现问题媒体/)).toBeVisible()
    })
  })

  it('尚未巡检时空态展示引导与可点巡检 CTA', async () => {
    let triggered = false
    server.use(
      http.get('*/api/library/health/status', () => HttpResponse.json(idleStatus)),
      http.get('*/api/library/health/issues', () => HttpResponse.json({ items: [] })),
      http.post('*/api/library/health/scan', () => {
        triggered = true
        return HttpResponse.json({ status: 'scanning' })
      }),
    )
    const user = userEvent.setup()
    renderPage()
    // 空态标题
    expect(await screen.findByText('尚未巡检')).toBeVisible()
    // 点击空态内的「开始巡检」CTA（与顶栏同名，取空态内的）
    const emptyState = screen.getByTestId('empty-state')
    await user.click(within(emptyState).getByRole('button', { name: /开始巡检/ }))
    await waitFor(() => expect(triggered).toBe(true))
  })

  it('按问题类型分组列出问题媒体', async () => {
    server.use(
      http.get('*/api/library/health/status', () => HttpResponse.json(completedStatus)),
      http.get('*/api/library/health/issues', () => HttpResponse.json({
        items: [
          makeIssue(1, 11, 'missing', '丢了.mp4'),
          makeIssue(2, 12, 'zero_byte', '空的.mp4'),
        ],
      })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('丢了.mp4')).toBeVisible()
    })
    expect(screen.getByText('空的.mp4')).toBeVisible()
    // 分组徽标
    expect(screen.getByText('源文件丢失')).toBeVisible()
    expect(screen.getByText('0 字节文件')).toBeVisible()
  })

  it('点击开始巡检调用触发接口', async () => {
    let triggered = false
    server.use(
      http.get('*/api/library/health/status', () => HttpResponse.json(idleStatus)),
      http.get('*/api/library/health/issues', () => HttpResponse.json({ items: [] })),
      http.post('*/api/library/health/scan', () => {
        triggered = true
        return HttpResponse.json({ status: 'scanning' })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    // 顶栏与空态 CTA 同名，点击顶栏（首个）即可触发
    const buttons = await screen.findAllByRole('button', { name: /开始巡检/ })
    await user.click(buttons[0])
    await waitFor(() => {
      expect(triggered).toBe(true)
    })
  })

  it('选中问题项后删除调用批量软删接口', async () => {
    let deletedIDs: number[] = []
    server.use(
      http.get('*/api/library/health/status', () => HttpResponse.json(completedStatus)),
      http.get('*/api/library/health/issues', () => HttpResponse.json({
        items: [makeIssue(1, 11, 'missing', '要删的.mp4')],
      })),
      http.post('*/api/library/media/batch-delete', async ({ request }) => {
        const body = await request.json() as { ids: number[] }
        deletedIDs = body.ids
        return HttpResponse.json({ deleted: body.ids.length })
      }),
    )

    const user = userEvent.setup()
    renderPage()

    // 勾选问题项
    const checkbox = await screen.findByRole('checkbox', { name: /要删的\.mp4/ })
    await user.click(checkbox)

    // 出现删除按钮并点击
    const delBtn = await screen.findByRole('button', { name: /删除选中/ })
    await user.click(delBtn)
    // 二次确认
    await user.click(await screen.findByRole('button', { name: '删除' }))

    await waitFor(() => {
      expect(deletedIDs).toEqual([11])
    })
  })

  it('巡检进行中展示进度', async () => {
    server.use(
      http.get('*/api/library/health/status', () => HttpResponse.json({ ...idleStatus, status: 'scanning', total: 10, checked: 4 })),
      http.get('*/api/library/health/issues', () => HttpResponse.json({ items: [] })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/巡检进行中/)).toBeVisible()
    })
    expect(screen.getByText('4 / 10')).toBeVisible()
  })
})
