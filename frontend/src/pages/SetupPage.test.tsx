import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi, beforeEach } from 'vitest'

import SetupPage from './SetupPage'

// mock react-router-dom 的 useNavigate
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

// mock @mantine/notifications
const mockNotificationShow = vi.fn()
vi.mock('@mantine/notifications', () => ({
  notifications: { show: (...args: unknown[]) => mockNotificationShow(...args) },
}))

// mock auth store
const mockSetup = vi.fn()
const mockClearError = vi.fn()
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    setup: mockSetup,
    loading: false,
    error: null,
    clearError: mockClearError,
  }),
}))

function renderSetupPage() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <SetupPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('SetupPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
  })

  it('渲染用户名/密码/确认密码与创建按钮', () => {
    renderSetupPage()
    expect(screen.getByRole('textbox', { name: /用户名/i })).toBeInTheDocument()
    expect(screen.getByLabelText('密码')).toBeInTheDocument()
    expect(screen.getByLabelText('确认密码')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /创建账号并进入/ })).toBeInTheDocument()
  })

  it('两次密码不一致时不提交', async () => {
    const user = userEvent.setup()
    renderSetupPage()

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'alice')
    await user.type(screen.getByLabelText('密码'), 'secret123')
    await user.type(screen.getByLabelText('确认密码'), 'mismatch1')
    await user.click(screen.getByRole('button', { name: /创建账号并进入/ }))

    await waitFor(() => {
      expect(screen.getByText('两次输入的密码不一致')).toBeInTheDocument()
    })
    expect(mockSetup).not.toHaveBeenCalled()
  })

  it('密码不足 6 位时不提交', async () => {
    const user = userEvent.setup()
    renderSetupPage()

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'alice')
    await user.type(screen.getByLabelText('密码'), '123')
    await user.type(screen.getByLabelText('确认密码'), '123')
    await user.click(screen.getByRole('button', { name: /创建账号并进入/ }))

    await waitFor(() => {
      expect(screen.getByText('密码至少 6 位')).toBeInTheDocument()
    })
    expect(mockSetup).not.toHaveBeenCalled()
  })

  it('合法输入提交调用 setup 并跳首页', async () => {
    const user = userEvent.setup()
    mockSetup.mockResolvedValue(undefined)
    renderSetupPage()

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'alice')
    await user.type(screen.getByLabelText('密码'), 'secret123')
    await user.type(screen.getByLabelText('确认密码'), 'secret123')
    await user.click(screen.getByRole('button', { name: /创建账号并进入/ }))

    await waitFor(() => {
      expect(mockSetup).toHaveBeenCalledWith('alice', 'secret123')
    })
    expect(mockNavigate).toHaveBeenCalledWith('/')
  })
})
