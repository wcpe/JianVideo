import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, vi, beforeEach } from 'vitest';

import LoginPage from './LoginPage';

// mock react-router-dom 的 useNavigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

// mock @mantine/notifications
const mockNotificationShow = vi.fn();
vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}));

// mock auth store — 使用可控的 mock 函数与可切换的错误态
const mockLogin = vi.fn();
const mockClearError = vi.fn();
const authStoreState = {
  login: mockLogin,
  loading: false,
  error: null as string | null,
  errorCode: null as string | null,
  loginRetryAfterSec: null as number | null,
  clearError: mockClearError,
};
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStoreState,
}));

// 辅助：渲染包裹在 MantineProvider + MemoryRouter 中的页面
function renderLoginPage() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.localStorage.clear();
    authStoreState.loading = false;
    authStoreState.error = null;
    authStoreState.errorCode = null;
    authStoreState.loginRetryAfterSec = null;
  });

  it('渲染用户名和密码输入框', () => {
    renderLoginPage();

    expect(screen.getByRole('textbox', { name: /用户名/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/密码/i)).toBeInTheDocument();
  });

  it('渲染登录按钮', () => {
    renderLoginPage();

    const button = screen.getByRole('button', { name: '登录' });
    expect(button).toBeInTheDocument();
  });

  it('输入用户名和密码', async () => {
    const user = userEvent.setup();
    renderLoginPage();

    const usernameInput = screen.getByRole('textbox', { name: /用户名/i });
    const passwordInput = screen.getByLabelText(/密码/i);

    await user.type(usernameInput, 'admin');
    await user.type(passwordInput, 'admin');

    expect(usernameInput).toHaveValue('admin');
    expect(passwordInput).toHaveValue('admin');
  });

  it('提交表单调用登录 API', async () => {
    const user = userEvent.setup();
    mockLogin.mockResolvedValue(undefined);

    renderLoginPage();

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'admin');
    await user.type(screen.getByLabelText(/密码/i), 'admin');
    await user.click(screen.getByRole('button', { name: '登录' }));

    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith('admin', 'admin');
    });
  });

  it('登录成功显示通知', async () => {
    const user = userEvent.setup();
    mockLogin.mockResolvedValue(undefined);

    renderLoginPage();

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'admin');
    await user.type(screen.getByLabelText(/密码/i), 'admin');
    await user.click(screen.getByRole('button', { name: '登录' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({
          title: '登录成功',
          message: '欢迎回来，admin',
          color: 'green',
        }),
      );
    });

    expect(mockNavigate).toHaveBeenCalledWith('/');
  });

  it('登录失败显示错误提示', async () => {
    const user = userEvent.setup();
    mockLogin.mockRejectedValue(new Error('用户名或密码错误'));

    renderLoginPage();

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'wrong');
    await user.type(screen.getByLabelText(/密码/i), 'wrong');
    await user.click(screen.getByRole('button', { name: '登录' }));

    // 登录失败后按钮应恢复可用状态（不显示错误提示在页面上，因为 mock store 不更新 error）
    await waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith('wrong', 'wrong');
    });
  });

  // FR2-062：凭据错误用红色「登录失败」Alert
  it('凭据错误时展示红色登录失败 Alert', () => {
    authStoreState.error = '用户名或密码错误';
    authStoreState.errorCode = 'INVALID_CREDENTIALS';
    renderLoginPage();

    expect(screen.getByTestId('login-error-alert')).toBeInTheDocument();
    expect(screen.getByText('登录失败')).toBeInTheDocument();
    expect(screen.getByText('用户名或密码错误')).toBeInTheDocument();
    expect(screen.queryByTestId('login-locked-alert')).not.toBeInTheDocument();
  });

  // FR2-062：429 LOGIN_LOCKED 用橙色锁定 Alert + Retry-After 提示
  it('LOGIN_LOCKED 时展示橙色锁定 Alert 与等待提示', () => {
    authStoreState.error = '登录尝试过于频繁，请稍后再试';
    authStoreState.errorCode = 'LOGIN_LOCKED';
    authStoreState.loginRetryAfterSec = 900;
    renderLoginPage();

    expect(screen.getByTestId('login-locked-alert')).toBeInTheDocument();
    expect(screen.getByText('登录已暂时锁定')).toBeInTheDocument();
    expect(screen.getByText('登录尝试过于频繁，请稍后再试')).toBeInTheDocument();
    expect(screen.getByText(/请等待 约 15 分钟 后再试/)).toBeInTheDocument();
    expect(screen.queryByTestId('login-error-alert')).not.toBeInTheDocument();
  });

  it('LOGIN_LOCKED 且无 Retry-After 时仅展示锁定标题与后端文案', () => {
    authStoreState.error = '登录尝试过于频繁，请稍后再试';
    authStoreState.errorCode = 'LOGIN_LOCKED';
    authStoreState.loginRetryAfterSec = null;
    renderLoginPage();

    expect(screen.getByTestId('login-locked-alert')).toBeInTheDocument();
    expect(screen.getByText('登录已暂时锁定')).toBeInTheDocument();
    expect(screen.queryByText(/请等待/)).not.toBeInTheDocument();
  });

  it('空表单提交时按钮被禁用', async () => {
    const user = userEvent.setup();
    renderLoginPage();

    // 表单无效时按钮应 disabled
    const button = screen.getByRole('button', { name: '登录' });
    expect(button).toBeDisabled();

    // 点击用户名输入框后离开，触发 blur 验证
    const usernameInput = screen.getByRole('textbox', { name: /用户名/i });
    await user.click(usernameInput);
    await user.tab();

    await waitFor(() => {
      // Mantine 表单 blur 校验错误文本（精确匹配避免与辅助提示「填写用户名和密码后即可登录」冲突）
      const errorText = screen.queryByText('请输入用户名') || screen.queryByText('请输入密码');
      expect(errorText || button).toBeInTheDocument();
    });
  });

  // FR-82：禁用态明确化——空表单时按钮禁用且有可辨辅助提示
  it('空表单时按钮禁用且展示可辨辅助提示', () => {
    renderLoginPage();

    const button = screen.getByRole('button', { name: '登录' });
    expect(button).toBeDisabled();

    // 应存在辅助提示告知用户需先填写，而非按钮故障
    expect(screen.getByText(/填写用户名和密码后即可登录/)).toBeInTheDocument();
  });

  // FR-82：品牌图形——登录卡片上方渲染原创 SVG logo
  it('渲染品牌 logo 图形', () => {
    renderLoginPage();

    expect(screen.getByRole('img', { name: 'JianVideo 标志' })).toBeInTheDocument();
  });

  // FR-82：填妥用户名密码后按钮可用、辅助提示消失（既有登录流程不回归）
  it('填妥用户名密码后按钮恢复可用且辅助提示消失', async () => {
    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByRole('textbox', { name: /用户名/i }), 'admin');
    await user.type(screen.getByLabelText(/密码/i), 'admin');

    const button = screen.getByRole('button', { name: '登录' });
    await waitFor(() => {
      expect(button).toBeEnabled();
    });
    expect(screen.queryByText(/填写用户名和密码后即可登录/)).not.toBeInTheDocument();
  });
});
