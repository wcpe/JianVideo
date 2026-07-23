import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useAuthStore } from './auth';

// 模拟 localStorage
const localStorageData: Record<string, string> = {};

const mockLocalStorage = {
  getItem: (key: string) => localStorageData[key] ?? null,
  setItem: (key: string, value: string) => {
    localStorageData[key] = value;
  },
  removeItem: (key: string) => {
    delete localStorageData[key];
  },
  clear: () => {
    Object.keys(localStorageData).forEach((k) => delete localStorageData[k]);
  },
};

Object.defineProperty(globalThis, 'localStorage', {
  value: mockLocalStorage,
  writable: true,
  configurable: true,
});

// 模拟 api/auth 模块
const mockLogin = vi.fn();
const mockLogout = vi.fn();
const mockGetMe = vi.fn();
const mockGetSetupStatus = vi.fn();
const mockSetup = vi.fn();

vi.mock('@/api/auth', () => ({
  login: (...args: unknown[]) => mockLogin(...args),
  logout: (...args: unknown[]) => mockLogout(...args),
  getMe: (...args: unknown[]) => mockGetMe(...args),
  getSetupStatus: (...args: unknown[]) => mockGetSetupStatus(...args),
  setup: (...args: unknown[]) => mockSetup(...args),
}));

describe('auth store', () => {
  beforeEach(() => {
    // 重置 store 状态
    useAuthStore.setState({
      username: null,
      isAuthenticated: false,
      loading: false,
      error: null,
      errorCode: null,
      loginRetryAfterSec: null,
      needsSetup: false,
      initialized: false,
    });
    // 清理 localStorage 模拟
    mockLocalStorage.clear();
    // 清理 mock
    vi.clearAllMocks();
    // 默认：系统已初始化（无需 setup），需 setup 的用例各自覆盖
    mockGetSetupStatus.mockResolvedValue({ needs_setup: false });
  });

  afterEach(() => {
    mockLocalStorage.clear();
  });

  it('初始状态为未认证', () => {
    const state = useAuthStore.getState();
    expect(state.username).toBeNull();
    expect(state.isAuthenticated).toBe(false);
    expect(state.loading).toBe(false);
    expect(state.error).toBeNull();
  });

  it('login 成功更新状态', async () => {
    mockLogin.mockResolvedValue(undefined);

    await useAuthStore.getState().login('admin', '123456');

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.username).toBe('admin');
    expect(state.loading).toBe(false);
    expect(state.error).toBeNull();
    expect(localStorage.getItem('jianvideo_auth')).toBe('1');
  });

  it('login 失败设置错误', async () => {
    mockLogin.mockRejectedValue(new Error('用户名或密码错误'));

    await expect(useAuthStore.getState().login('admin', 'wrong')).rejects.toThrow(
      '用户名或密码错误',
    );

    const state = useAuthStore.getState();
    expect(state.error).toBe('用户名或密码错误');
    expect(state.isAuthenticated).toBe(false);
    expect(state.loading).toBe(false);
  });

  it('login 失败优先提取 axios 响应体 message', async () => {
    mockLogin.mockRejectedValue({
      response: { data: { message: '用户名或密码错误' } },
      message: 'Request failed with status code 401',
    });

    await expect(useAuthStore.getState().login('admin', 'wrong')).rejects.toBeTruthy();

    const state = useAuthStore.getState();
    expect(state.error).toBe('用户名或密码错误');
    expect(state.isAuthenticated).toBe(false);
    expect(state.loading).toBe(false);
  });

  // FR2-062：429 LOGIN_LOCKED 提取 code 与 Retry-After
  it('login 429 LOGIN_LOCKED 设置 errorCode 与 loginRetryAfterSec', async () => {
    mockLogin.mockRejectedValue({
      response: {
        status: 429,
        data: { code: 'LOGIN_LOCKED', message: '登录尝试过于频繁，请稍后再试' },
        headers: { 'retry-after': '900' },
      },
      message: 'Request failed with status code 429',
    });

    await expect(useAuthStore.getState().login('admin', 'wrong')).rejects.toBeTruthy();

    const state = useAuthStore.getState();
    expect(state.error).toBe('登录尝试过于频繁，请稍后再试');
    expect(state.errorCode).toBe('LOGIN_LOCKED');
    expect(state.loginRetryAfterSec).toBe(900);
    expect(state.isAuthenticated).toBe(false);
    expect(state.loading).toBe(false);
  });

  it('login 非锁定失败不写入 loginRetryAfterSec', async () => {
    mockLogin.mockRejectedValue({
      response: {
        status: 401,
        data: { code: 'INVALID_CREDENTIALS', message: '用户名或密码错误' },
        headers: { 'retry-after': '60' },
      },
    });

    await expect(useAuthStore.getState().login('admin', 'wrong')).rejects.toBeTruthy();

    const state = useAuthStore.getState();
    expect(state.errorCode).toBe('INVALID_CREDENTIALS');
    expect(state.loginRetryAfterSec).toBeNull();
  });

  it('logout 清除状态', async () => {
    // 先登录
    mockLogin.mockResolvedValue(undefined);
    mockLogout.mockResolvedValue(undefined);
    await useAuthStore.getState().login('admin', '123456');

    // 再登出
    await useAuthStore.getState().logout();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.username).toBeNull();
    expect(state.error).toBeNull();
    expect(localStorage.getItem('jianvideo_auth')).toBeNull();
  });

  it('clearAuth 清除认证状态', () => {
    // 手动设置已认证状态
    useAuthStore.setState({ username: 'admin', isAuthenticated: true });
    localStorage.setItem('jianvideo_auth', '1');

    useAuthStore.getState().clearAuth();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.username).toBeNull();
    expect(state.error).toBeNull();
    expect(localStorage.getItem('jianvideo_auth')).toBeNull();
  });

  it('clearError 清除错误', () => {
    useAuthStore.setState({
      error: 'some error',
      errorCode: 'LOGIN_LOCKED',
      loginRetryAfterSec: 60,
    });

    useAuthStore.getState().clearError();

    const state = useAuthStore.getState();
    expect(state.error).toBeNull();
    expect(state.errorCode).toBeNull();
    expect(state.loginRetryAfterSec).toBeNull();
  });

  it('init 从 localStorage 恢复登录态', async () => {
    localStorage.setItem('jianvideo_auth', '1');
    mockGetMe.mockResolvedValue({ username: 'admin' });

    await useAuthStore.getState().init();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(true);
    expect(state.username).toBe('admin');
  });

  it('init 无 localStorage 时不恢复', async () => {
    // localStorage 无 jianvideo_auth（beforeEach 已清理）

    await useAuthStore.getState().init();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(mockGetMe).not.toHaveBeenCalled();
  });

  it('init 执行后标记 initialized 为 true', async () => {
    await useAuthStore.getState().init();

    expect(useAuthStore.getState().initialized).toBe(true);
  });

  it('init 检测到需初始化时置 needsSetup 并跳过登录态恢复（FR-109）', async () => {
    mockGetSetupStatus.mockResolvedValue({ needs_setup: true });
    // 即便本地有旧凭据，也应被忽略并导向初始化
    localStorage.setItem('jianvideo_auth', '1');

    await useAuthStore.getState().init();

    const state = useAuthStore.getState();
    expect(state.needsSetup).toBe(true);
    expect(state.isAuthenticated).toBe(false);
    expect(state.initialized).toBe(true);
    expect(mockGetMe).not.toHaveBeenCalled();
    expect(localStorage.getItem('jianvideo_auth')).toBeNull();
  });

  it('setup 成功创建账户并自动登录（FR-109）', async () => {
    useAuthStore.setState({ needsSetup: true });
    mockSetup.mockResolvedValue(undefined);

    await useAuthStore.getState().setup('alice', 'secret123');

    const state = useAuthStore.getState();
    expect(mockSetup).toHaveBeenCalledWith('alice', 'secret123');
    expect(state.isAuthenticated).toBe(true);
    expect(state.username).toBe('alice');
    expect(state.needsSetup).toBe(false);
    expect(state.loading).toBe(false);
    expect(localStorage.getItem('jianvideo_auth')).toBe('1');
  });

  it('setup 失败设置错误', async () => {
    mockSetup.mockRejectedValue(new Error('系统已初始化'));

    await expect(useAuthStore.getState().setup('alice', 'secret123')).rejects.toThrow(
      '系统已初始化',
    );

    const state = useAuthStore.getState();
    expect(state.error).toBe('系统已初始化');
    expect(state.isAuthenticated).toBe(false);
    expect(state.loading).toBe(false);
  });
});
