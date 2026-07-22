import { describe, it, expect, vi, beforeEach } from 'vitest';

// mock @mantine/notifications，捕获 show 调用
const mockNotificationShow = vi.fn();
vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}));

import { extractErrorMessage, handleApiError } from './error';

describe('extractErrorMessage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('从 axios 响应体提取 message', () => {
    const error = { response: { data: { message: '目录不存在' } } };
    expect(extractErrorMessage(error)).toBe('目录不存在');
  });

  it('从 Error 实例提取 message', () => {
    expect(extractErrorMessage(new Error('解析失败'))).toBe('解析失败');
  });

  it('未知值回退到默认文案', () => {
    expect(extractErrorMessage(null)).toBe('请求失败');
    expect(extractErrorMessage('字符串错误', '自定义回退')).toBe('自定义回退');
  });
});

describe('handleApiError', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('调用 notifications.show 弹出红色提示并返回信息', () => {
    const error = { response: { data: { message: '保存失败' } } };

    const result = handleApiError(error);

    expect(result).toBe('保存失败');
    expect(mockNotificationShow).toHaveBeenCalledWith(
      expect.objectContaining({
        title: '操作失败',
        message: '保存失败',
        color: 'red',
      }),
    );
  });

  it('无响应时使用传入的回退文案', () => {
    const result = handleApiError(new Error(''), '网络异常，请检查连接后重试');

    expect(result).toBe('网络异常，请检查连接后重试');
    expect(mockNotificationShow).toHaveBeenCalledWith(
      expect.objectContaining({ message: '网络异常，请检查连接后重试', color: 'red' }),
    );
  });
});
