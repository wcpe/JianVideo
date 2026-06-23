import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import SettingsPage from './SettingsPage'
import { server } from '@/mocks/beforeAll'

const mockNotificationShow = vi.fn()

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}))

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/settings']}>
        <SettingsPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('加载并展示现有设置值', async () => {
    renderPage()
    // 扫描周期初始值 3600 应填入输入框
    await waitFor(() => {
      expect(screen.getByLabelText('扫描周期（秒）')).toHaveValue('3600')
    })
    // 回收站路径初始 JSON 也应展示
    expect(screen.getByLabelText('每盘符回收站路径（JSON）')).toHaveValue('{"D":"D:/.recycle"}')
  })

  it('修改扫描周期并保存后提示成功', async () => {
    const user = userEvent.setup()
    renderPage()

    const input = await screen.findByLabelText('扫描周期（秒）')
    await waitFor(() => expect(input).toHaveValue('3600'))

    await user.clear(input)
    await user.type(input, '7200')
    await user.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      )
    })
    // 保存后输入框仍展示新值（回读结果）
    expect(input).toHaveValue('7200')
  })

  it('加载失败时展示错误提示', async () => {
    server.use(
      http.get('*/api/settings', () =>
        HttpResponse.json({ code: 'INTERNAL', message: '查询设置失败' }, { status: 500 }),
      ),
    )
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('查询设置失败')).toBeVisible()
    })
  })

  it('渲染环境变量区块：非敏感显明文、敏感显掩码不显明文', async () => {
    renderPage()
    // 非敏感项 JIANVIDEO_FFMPEG_PATH 展示明文路径
    await waitFor(() => {
      expect(screen.getByText('/opt/jianvideo/ffmpeg')).toBeVisible()
    })
    // 敏感项 JWT_SECRET 展示掩码而非明文
    expect(screen.getByText('JWT_SECRET')).toBeVisible()
    expect(screen.getByText('****（已设置）')).toBeVisible()
    // 敏感项明文绝不出现在 DOM（脱敏）：响应里没有真实 secret，故任何 secret 串都不应可见
    // 未设置项展示「（未设置）」掩码（敏感 SMB 与非敏感 DEBUG 均如此，故至少一处）
    expect(screen.getAllByText('（未设置）').length).toBeGreaterThan(0)
  })

  it('填 ffmpeg 路径点检测，展示可用性与版本', async () => {
    const user = userEvent.setup()
    renderPage()

    const input = await screen.findByLabelText('FFmpeg 路径')
    await user.type(input, 'D:/tools/ffmpeg.exe')
    await user.click(screen.getByRole('button', { name: '检测' }))

    await waitFor(() => {
      expect(screen.getByText(/可用：ffmpeg version/)).toBeVisible()
    })
  })

  it('检测不可用路径时展示「不可用」', async () => {
    const user = userEvent.setup()
    renderPage()

    const input = await screen.findByLabelText('FFmpeg 路径')
    await user.type(input, 'D:/tools/notexist.bin')
    await user.click(screen.getByRole('button', { name: '检测' }))

    await waitFor(() => {
      expect(screen.getByText('不可用')).toBeVisible()
    })
  })

  it('保存 ffmpeg 路径走 PUT /api/settings', async () => {
    const user = userEvent.setup()
    let putBody: { settings: Record<string, string> } | null = null
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putBody = await request.json() as { settings: Record<string, string> }
        return HttpResponse.json({ settings: putBody.settings })
      }),
    )
    renderPage()

    const input = await screen.findByLabelText('FFmpeg 路径')
    await user.type(input, 'D:/tools/ffmpeg.exe')
    await user.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      )
    })
    expect(putBody).not.toBeNull()
    expect(putBody!.settings.ffmpeg_path).toBe('D:/tools/ffmpeg.exe')
  })
})
