import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import TranscodePage from './TranscodePage'
import { server } from '@/mocks/beforeAll'
import type { TranscodePreset, TranscodeTask } from '@/types'

const mockNotificationShow = vi.fn()

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}))

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/transcode']}>
        <TranscodePage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

const samplePreset: TranscodePreset = {
  id: 1, name: '1080p HEVC', codec: 'h265', width: 1920, height: 1080,
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
}

// 默认 stub：空预设、空任务，避免轮询触发未处理请求
function stubEmpty() {
  server.use(
    http.get('*/api/transcode/presets', () => HttpResponse.json({ items: [] })),
    http.get('*/api/transcode/tasks', () => HttpResponse.json({ tasks: [] })),
  )
}

describe('TranscodePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染标题与新建按钮', async () => {
    stubEmpty()
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('转码预设')).toBeVisible()
      // 顶栏与空态 CTA 同名「新建预设」，至少有一个可见
      expect(screen.getAllByRole('button', { name: '新建预设' }).length).toBeGreaterThan(0)
    })
  })

  it('列出已有预设', async () => {
    server.use(
      http.get('*/api/transcode/presets', () => HttpResponse.json({ items: [samplePreset] })),
      http.get('*/api/transcode/tasks', () => HttpResponse.json({ tasks: [] })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('1080p HEVC')).toBeVisible()
      expect(screen.getByText('H265')).toBeVisible()
    })
  })

  it('新建预设并提交', async () => {
    let payload: { name?: string; codec?: string } | null = null
    server.use(
      http.get('*/api/transcode/presets', () => HttpResponse.json({ items: [] })),
      http.get('*/api/transcode/tasks', () => HttpResponse.json({ tasks: [] })),
      http.post('*/api/transcode/presets', async ({ request }) => {
        payload = await request.json() as { name: string; codec: string }
        return HttpResponse.json({ ...samplePreset, name: payload.name, codec: payload.codec }, { status: 201 })
      }),
    )
    renderPage()

    // 顶栏与空态 CTA 同名「新建预设」，点击顶栏（首个）打开弹窗
    const newButtons = await screen.findAllByRole('button', { name: '新建预设' })
    await userEvent.click(newButtons[0])
    const nameInput = await screen.findByLabelText(/预设名称/)
    await userEvent.type(nameInput, '我的预设')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(payload).not.toBeNull()
      expect(payload?.name).toBe('我的预设')
    })
  })

  it('无预设时空态显示引导与新建 CTA，点击打开新建弹窗', async () => {
    stubEmpty()
    const user = userEvent.setup()
    renderPage()
    // 空态引导文案
    expect(await screen.findByText('还没有转码预设')).toBeVisible()
    // 空态内的「新建预设」CTA（与顶栏同名按钮共存，取空态卡片内的）
    const ctaButtons = await screen.findAllByRole('button', { name: '新建预设' })
    // 点击其中一个触发新建弹窗
    await user.click(ctaButtons[ctaButtons.length - 1])
    expect(await screen.findByLabelText(/预设名称/)).toBeVisible()
  })

  it('无预生成任务时空态显示引导文案', async () => {
    stubEmpty()
    renderPage()
    expect(await screen.findByText('暂无预生成任务')).toBeVisible()
  })

  it('渲染预生成队列任务', async () => {
    const task: TranscodeTask = {
      id: 7, media_id: 42, preset_id: 1, codec: 'h265', width: 1920, height: 1080,
      status: 'completed', error: '', created_at: '2026-01-01T00:00:00Z',
    }
    server.use(
      http.get('*/api/transcode/presets', () => HttpResponse.json({ items: [] })),
      http.get('*/api/transcode/tasks', () => HttpResponse.json({ tasks: [task] })),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText(/媒体 #42/)).toBeVisible()
      expect(screen.getByText('已完成')).toBeVisible()
    })
  })
})
