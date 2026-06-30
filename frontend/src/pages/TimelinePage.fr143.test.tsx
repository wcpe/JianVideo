import { render, screen, waitFor, within, fireEvent, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import TimelinePage from './TimelinePage';
import { server } from '@/mocks/beforeAll';

// FR-143 移动端交互：筛选抽屉一键重置 + 窄屏下拉刷新。

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return { ...actual, useNavigate: () => vi.fn() };
});

vi.mock('@mantine/notifications', () => ({
  notifications: { show: vi.fn() },
}));

/** 让 matchMedia 对窄屏查询返回 true，以启用下拉刷新（FR-143） */
function mockNarrowViewport(narrow: boolean) {
  window.matchMedia = ((query: string) => ({
    matches: narrow && /max-width/.test(query),
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/']}>
        <TimelinePage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('TimelinePage 筛选抽屉一键重置（FR-143）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockNarrowViewport(false);
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 }),
      ),
      http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })),
    );
  });

  it('抽屉内「重置筛选」无筛选时禁用、选筛选后启用，点击清空并关抽屉', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByPlaceholderText(/搜索：文件名/);

    await user.click(screen.getByRole('button', { name: '筛选' }));
    const dialog = await screen.findByRole('dialog');

    // 无筛选生效 → 重置按钮禁用
    const resetBtn = within(dialog).getByRole('button', { name: '重置筛选' });
    expect(resetBtn).toBeDisabled();

    // 选「收藏夹」分类（FR-139）→ 重置按钮启用
    await user.selectOptions(within(dialog).getByRole('combobox', { name: '分类' }), 'favorite');
    await waitFor(() => expect(resetBtn).toBeEnabled());

    // 点击重置 → 抽屉关闭（清空由页面状态承接）
    await user.click(resetBtn);
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    // 再次打开 → 分类回到「全部」
    await user.click(screen.getByRole('button', { name: '筛选' }));
    const dialog2 = await screen.findByRole('dialog');
    expect(within(dialog2).getByRole('combobox', { name: '分类' })).toHaveValue('all');
  });
});

describe('TimelinePage 窄屏下拉刷新（FR-143）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.use(http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })));
    Object.defineProperty(window, 'scrollY', { value: 0, writable: true, configurable: true });
  });

  afterEach(() => mockNarrowViewport(false));

  it('窄屏顶部下拉松手重载首屏（再次请求 media）', async () => {
    mockNarrowViewport(true);
    let mediaCalls = 0;
    server.use(
      http.get('*/api/library/media', () => {
        mediaCalls += 1;
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 });
      }),
    );
    renderPage();
    // 等首屏请求完成
    await waitFor(() => expect(mediaCalls).toBeGreaterThan(0));
    const before = mediaCalls;

    const region = screen.getByTestId('pull-to-refresh');
    fireEvent.touchStart(region, { touches: [{ clientY: 0 }] });
    fireEvent.touchMove(region, { touches: [{ clientY: 120 }] });
    await act(async () => {
      fireEvent.touchEnd(region, { changedTouches: [{ clientY: 120 }] });
    });

    // 下拉刷新触发 reload → 新一轮 media 请求
    await waitFor(() => expect(mediaCalls).toBeGreaterThan(before));
  });
});
