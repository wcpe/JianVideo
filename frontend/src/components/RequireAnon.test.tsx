import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, beforeEach } from 'vitest';

import RequireAnon from './RequireAnon';
import { useAuthStore } from '@/stores/auth';

// 渲染匿名内容，并提供 / 占位路由以验证已登录时的重定向
function renderAnon() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route
            path="/login"
            element={
              <RequireAnon>
                <div>登录页占位</div>
              </RequireAnon>
            }
          />
          <Route path="/" element={<div>首页占位</div>} />
          <Route path="/setup" element={<div>初始化引导占位</div>} />
        </Routes>
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('RequireAnon', () => {
  beforeEach(() => {
    // 直接设置 initialized:true 跳过 init，避免触发真实认证恢复
    useAuthStore.setState({
      initialized: true,
      isAuthenticated: false,
      username: null,
      needsSetup: false,
    });
  });

  it('需首次初始化时重定向到 /setup（FR-109）', () => {
    useAuthStore.setState({ initialized: true, isAuthenticated: false, needsSetup: true });

    renderAnon();

    expect(screen.getByText('初始化引导占位')).toBeInTheDocument();
    expect(screen.queryByText('登录页占位')).toBeNull();
  });

  it('已初始化且已认证时重定向到 /', () => {
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' });

    renderAnon();

    expect(screen.getByText('首页占位')).toBeInTheDocument();
    expect(screen.queryByText('登录页占位')).toBeNull();
  });

  it('已初始化且未认证时渲染匿名内容', () => {
    useAuthStore.setState({ initialized: true, isAuthenticated: false });

    renderAnon();

    expect(screen.getByText('登录页占位')).toBeInTheDocument();
    expect(screen.queryByText('首页占位')).toBeNull();
  });
});
