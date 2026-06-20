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
})
