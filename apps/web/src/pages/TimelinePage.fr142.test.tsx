import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import TimelinePage from './TimelinePage';
import { server } from '@/mocks/beforeAll';

// FR-142 时间轴精准定位：日期跳转输入 + 粒度切换视口锁定。
// TimelineView 句柄经命令式调用，jsdom 无真实布局，故此处验证：
// - 日期输入框存在、非法输入给出反馈不跳转；
// - 句柄被正确调用（间谍化 scrollToDate / getTopVisibleGroupDate）。

const mockNotificationShow = vi.fn();
const scrollToDate = vi.fn().mockReturnValue(true);
const getTopVisibleGroupDate = vi.fn().mockReturnValue('2025-01-09');

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}));

// 以轻量替身替换 TimelineView，仅暴露 FR-142 句柄供页面驱动，避免依赖真实虚拟化布局。
vi.mock('@/components/TimelineView', async () => {
  const { forwardRef, useImperativeHandle } = await import('react');
  const Stub = forwardRef((_props: Record<string, unknown>, ref: React.Ref<unknown>) => {
    useImperativeHandle(ref, () => ({ scrollToDate, getTopVisibleGroupDate }));
    return null;
  });
  Stub.displayName = 'TimelineViewStub';
  return { default: Stub };
});

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/']}>
        <TimelinePage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('TimelinePage 日期跳转与粒度切换视口锁定（FR-142）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    scrollToDate.mockReturnValue(true);
    getTopVisibleGroupDate.mockReturnValue('2025-01-09');
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 }),
      ),
    );
  });

  it('提供「跳转到日期」输入框', async () => {
    renderPage();
    expect(await screen.findByLabelText('跳转到日期')).toBeInTheDocument();
  });

  it('输入有效日期回车后调用 scrollToDate 跳转', async () => {
    const user = userEvent.setup();
    renderPage();
    const input = await screen.findByLabelText('跳转到日期');
    await user.type(input, '2025-01{Enter}');
    expect(scrollToDate).toHaveBeenCalledWith('2025-01');
  });

  it('输入非法日期不跳转并提示', async () => {
    scrollToDate.mockReturnValue(false);
    const user = userEvent.setup();
    renderPage();
    const input = await screen.findByLabelText('跳转到日期');
    await user.type(input, 'abc{Enter}');
    expect(scrollToDate).toHaveBeenCalledWith('abc');
    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalled();
    });
  });

  it('切换粒度前记录视口日期、切换后据其重新定位（视口锁定）', async () => {
    const user = userEvent.setup();
    renderPage();
    // 等首屏渲染稳定
    await screen.findByLabelText('跳转到日期');
    getTopVisibleGroupDate.mockReturnValue('2025-01-09');

    // 切到月粒度：应先读视口日期，再在切换后用该日期调用 scrollToDate 锁定视口
    await user.click(screen.getByRole('radio', { name: '月' }));
    await waitFor(() => {
      expect(getTopVisibleGroupDate).toHaveBeenCalled();
      expect(scrollToDate).toHaveBeenCalledWith('2025-01-09');
    });
  });

  it('切到「所有」粒度不做视口锁定（单组无意义）', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText('跳转到日期');
    scrollToDate.mockClear();

    await user.click(screen.getByRole('radio', { name: '所有' }));
    // 给副作用一帧时间，确认未因粒度切换触发日期跳转
    await new Promise((r) => setTimeout(r, 30));
    expect(scrollToDate).not.toHaveBeenCalled();
  });
});
