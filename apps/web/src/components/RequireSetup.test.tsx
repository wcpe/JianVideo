import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, beforeEach } from 'vitest';

import RequireSetup from './RequireSetup';
import { useAuthStore } from '@/stores/auth';

function renderSetupGuard() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/setup']}>
        <Routes>
          <Route
            path="/setup"
            element={
              <RequireSetup>
                <div>初始化引导占位</div>
              </RequireSetup>
            }
          />
          <Route path="/login" element={<div>登录页占位</div>} />
        </Routes>
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('RequireSetup', () => {
  beforeEach(() => {
    useAuthStore.setState({
      initialized: true,
      isAuthenticated: false,
      username: null,
      needsSetup: false,
    });
  });

  it('需初始化时渲染初始化引导', () => {
    useAuthStore.setState({ initialized: true, needsSetup: true });

    renderSetupGuard();

    expect(screen.getByText('初始化引导占位')).toBeInTheDocument();
    expect(screen.queryByText('登录页占位')).toBeNull();
  });

  it('已初始化（无需 setup）时重定向到 /login', () => {
    useAuthStore.setState({ initialized: true, needsSetup: false });

    renderSetupGuard();

    expect(screen.getByText('登录页占位')).toBeInTheDocument();
    expect(screen.queryByText('初始化引导占位')).toBeNull();
  });
});
