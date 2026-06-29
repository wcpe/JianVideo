import { describe, it, expect, vi, beforeEach } from 'vitest';

// 桩 @mantine/notifications，捕获 show 入参以断言统一约定
const showMock = vi.fn();
vi.mock('@mantine/notifications', () => ({ notifications: { show: (o: unknown) => showMock(o) } }));

import { notifySuccess, notifyError } from './notify';

describe('notify 通知样式统一（FR-98）', () => {
  beforeEach(() => showMock.mockClear());

  it('notifySuccess 用绿色与较短自动关闭，带图标', () => {
    notifySuccess('已保存', '成功');
    expect(showMock).toHaveBeenCalledTimes(1);
    const arg = showMock.mock.calls[0][0];
    expect(arg.color).toBe('green');
    expect(arg.message).toBe('已保存');
    expect(arg.title).toBe('成功');
    expect(arg.autoClose).toBe(2500);
    expect(arg.icon).toBeTruthy();
  });

  it('notifyError 用红色与较长自动关闭，带图标', () => {
    notifyError('保存失败');
    const arg = showMock.mock.calls[0][0];
    expect(arg.color).toBe('red');
    expect(arg.message).toBe('保存失败');
    expect(arg.autoClose).toBe(4000);
    expect(arg.icon).toBeTruthy();
  });
});
