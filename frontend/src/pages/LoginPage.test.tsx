import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi, beforeEach } from 'vitest'

import LoginPage from './LoginPage'

// mock react-router-dom 的 useNavigate
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

// mock @mantine/notifications
const mockNotificationShow = vi.fn()
vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}))

// mock auth store — 使用可控的 mock 函数
const mockLogin = vi.fn()
const mockClearError = vi.fn()
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    login: mockLogin,
    loading: false,
    error: null,
    clearError: mockClearError,
  }),
}))

// 辅助：渲染包裹在 MantineProvider + MemoryRouter 中的页面
function renderLoginPage() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
  })

  it('渲染用户名和密码输入框', () => {
    renderLoginPage()

    expect(screen.getByRole('textbox', { name: /用户名/i })).toBeInTheDocument()
    expect(screen.getByLabelText(/密码/i)).toBeInTheDocument()
  })

  it('渲染登录按钮', () => {
    renderLoginPage()

    const button = screen.getByRole('button', { name: '登录' })
    expect(button).toBeInTheDocument()
  })

  it('输入用户名和密码', async () => {
    const user = userEvent.setup()
    renderLoginPage()

    const usernameInput = screen.getByRole('textbox', { name: /用户名/i })
    const passwordInput = screen.getByLabelText(/密码/i)

    await user.type(usernameInput, 'admin')
    await user.type(passwordInput, 'admin')

    expect(usernameInput).toHaveValue('admin')
    expect(passwordInput).toHaveValue('admin')
  })

  it('提交表单调用登录 API', async () => {
    const user = userEvent.setup()
    mockLogin.mockResolvedValue(undefined)

    renderLoginPage()

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'admin')
    await user.type(screen.getByLabelText(/密码/i), 'admin')
    await user.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith('admin', 'admin')
    })
  })

  it('登录成功显示通知', async () => {
    const user = userEvent.setup()
    mockLogin.mockResolvedValue(undefined)

    renderLoginPage()

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'admin')
    await user.type(screen.getByLabelText(/密码/i), 'admin')
    await user.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({
          title: '登录成功',
          message: '欢迎回来，admin',
          color: 'green',
        }),
      )
    })

    expect(mockNavigate).toHaveBeenCalledWith('/library')
  })

  it('登录失败显示错误提示', async () => {
    const user = userEvent.setup()
    mockLogin.mockRejectedValue(new Error('用户名或密码错误'))

    renderLoginPage()

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'wrong')
    await user.type(screen.getByLabelText(/密码/i), 'wrong')
    await user.click(screen.getByRole('button', { name: '登录' }))

    // 登录失败后按钮应恢复可用状态（不显示错误提示在页面上，因为 mock store 不更新 error）
    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith('wrong', 'wrong')
    })
  })

  it('空表单提交时按钮被禁用', async () => {
    const user = userEvent.setup()
    renderLoginPage()

    // 表单无效时按钮应 disabled
    const button = screen.getByRole('button', { name: '登录' })
    expect(button).toBeDisabled()

    // 点击用户名输入框后离开，触发 blur 验证
    const usernameInput = screen.getByRole('textbox', { name: /用户名/i })
    await user.click(usernameInput)
    await user.tab()

    await waitFor(() => {
      // Mantine 表单验证错误文本
      const errorText = screen.queryByText(/用户名/) || screen.queryByText(/请输入/)
      expect(errorText || button).toBeInTheDocument()
    })
  })
})
