import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, beforeEach, vi } from 'vitest'

import AppLayout from './AppLayout'
import { useAuthStore } from '@/stores/auth'

// mock react-router-dom 的 useNavigate，避免真实跳转
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

function renderLayout() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <AppLayout>
          <div>页面内容</div>
        </AppLayout>
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('AppLayout 主题切换', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // 标记已认证，避免守卫相关逻辑干扰
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  it('渲染切换主题按钮', () => {
    renderLayout()

    expect(screen.getByRole('button', { name: '切换主题' })).toBeInTheDocument()
  })

  it('点击切换主题按钮不报错', async () => {
    const user = userEvent.setup()
    renderLayout()

    const toggle = screen.getByRole('button', { name: '切换主题' })
    await user.click(toggle)

    // 点击后按钮仍在文档中，确认交互未抛错
    expect(toggle).toBeInTheDocument()
  })
})
