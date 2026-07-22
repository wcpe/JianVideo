import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';

import { server } from '@/mocks/beforeAll';

// 拦截全局错误处理：断言网络错误时是否弹出全局 toast
const handleApiError = vi.fn();
const extractErrorMessage = vi.fn((_: unknown, fallback: string) => fallback);
vi.mock('@/utils/error', () => ({
  handleApiError: (...args: unknown[]) => handleApiError(...args),
  extractErrorMessage: (...args: unknown[]) => extractErrorMessage(...(args as [unknown, string])),
}));

const clearAuth = vi.fn();
vi.mock('@/stores/auth', () => ({
  useAuthStore: {
    getState: () => ({
      token: undefined,
      clearAuth: (...args: unknown[]) => clearAuth(...args),
    }),
  },
}));

// 被测模块在 mock 之后再导入，确保拦截器内引用的是 mock 版本
import client, { locationPath } from './client';

describe('axios 客户端网络错误 toast 去重（FR-115 后续修复·通知白屏）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('普通请求网络失败：弹一次全局网络 toast', async () => {
    // 用 msw 模拟网络层错误（无响应）
    server.use(http.get('*/api/test-normal', () => HttpResponse.error()));

    await expect(client.get('/api/test-normal')).rejects.toBeTruthy();
    expect(handleApiError).toHaveBeenCalledTimes(1);
  });

  it('带 silent 标记的请求网络失败：不弹全局网络 toast（防轮询失败堆积白屏）', async () => {
    server.use(http.get('*/api/test-silent', () => HttpResponse.error()));

    await expect(client.get('/api/test-silent', { silent: true })).rejects.toBeTruthy();
    expect(handleApiError).not.toHaveBeenCalled();
  });
});

describe('axios 客户端 401 分流（禁止硬刷登录页）', () => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let pushStateSpy: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let dispatchSpy: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let pathGetSpy: any;

  beforeEach(() => {
    vi.clearAllMocks();
    extractErrorMessage.mockImplementation((_: unknown, fallback: string) => fallback);
    // 通过 locationPath 注入当前路径，避开 jsdom 对 location 的限制
    pathGetSpy = vi.spyOn(locationPath, 'get').mockReturnValue('/browse');
    pushStateSpy = vi.spyOn(window.history, 'pushState').mockImplementation(() => undefined);
    dispatchSpy = vi.spyOn(window, 'dispatchEvent');
  });

  afterEach(() => {
    pathGetSpy?.mockRestore?.();
    pushStateSpy?.mockRestore?.();
    dispatchSpy?.mockRestore?.();
  });

  it('登录 401：不清态、不 toast、不跳转，交给页面展示错误', async () => {
    server.use(
      http.post('*/api/auth/login', () =>
        HttpResponse.json({ code: 'UNAUTHORIZED', message: '用户名或密码错误' }, { status: 401 }),
      ),
    );

    await expect(
      client.post('/api/auth/login', { username: 'a', password: 'b' }),
    ).rejects.toBeTruthy();
    expect(clearAuth).not.toHaveBeenCalled();
    expect(handleApiError).not.toHaveBeenCalled();
    expect(pushStateSpy).not.toHaveBeenCalled();
  });

  it('受保护接口 401：toast 展示原因 + 清态 + SPA 软跳 /login（非硬刷）', async () => {
    extractErrorMessage.mockReturnValue('令牌无效或已过期');
    server.use(
      http.get('*/api/config', () =>
        HttpResponse.json({ code: 'UNAUTHORIZED', message: '令牌无效或已过期' }, { status: 401 }),
      ),
    );

    await expect(client.get('/api/config')).rejects.toBeTruthy();
    expect(clearAuth).toHaveBeenCalledTimes(1);
    expect(handleApiError).toHaveBeenCalledTimes(1);
    expect(pushStateSpy).toHaveBeenCalledWith({}, '', '/login');
    expect(dispatchSpy).toHaveBeenCalledWith(expect.objectContaining({ type: 'popstate' }));
  });

  it('已在 /login 时受保护 401：不重复 pushState', async () => {
    pathGetSpy.mockReturnValue('/login');
    server.use(
      http.get('*/api/me', () =>
        HttpResponse.json({ code: 'UNAUTHORIZED', message: '请先登录' }, { status: 401 }),
      ),
    );

    await expect(client.get('/api/me')).rejects.toBeTruthy();
    expect(clearAuth).toHaveBeenCalledTimes(1);
    expect(pushStateSpy).not.toHaveBeenCalled();
  });
});
