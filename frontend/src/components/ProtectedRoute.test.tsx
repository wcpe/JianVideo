import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, beforeEach } from 'vitest'

import ProtectedRoute from './ProtectedRoute'
import { useAuthStore } from '@/stores/auth'

// 渲染受保护内容，并提供 /login 占位路由以验证重定向
function renderGuarded() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <div>受保护内容</div>
              </ProtectedRoute>
            }
          />
          <Route path="/login" element={<div>登录页占位</div>} />
          <Route path="/setup" element={<div>初始化引导占位</div>} />
        </Routes>
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('ProtectedRoute', () => {
  beforeEach(() => {
    // 直接设置 initialized:true 跳过 init，避免触发真实认证恢复
    useAuthStore.setState({ initialized: true, isAuthenticated: false, username: null, needsSetup: false })
  })

  it('需首次初始化时重定向到 /setup（FR-109）', () => {
    useAuthStore.setState({ initialized: true, isAuthenticated: false, needsSetup: true })

    renderGuarded()

    expect(screen.getByText('初始化引导占位')).toBeInTheDocument()
    expect(screen.queryByText('受保护内容')).toBeNull()
    expect(screen.queryByText('登录页占位')).toBeNull()
  })

  it('已初始化但未认证时重定向到 /login', () => {
    useAuthStore.setState({ initialized: true, isAuthenticated: false })

    renderGuarded()

    expect(screen.getByText('登录页占位')).toBeInTheDocument()
    expect(screen.queryByText('受保护内容')).toBeNull()
  })

  it('已初始化且已认证时渲染受保护内容', () => {
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })

    renderGuarded()

    expect(screen.getByText('受保护内容')).toBeInTheDocument()
    expect(screen.queryByText('登录页占位')).toBeNull()
  })
})
