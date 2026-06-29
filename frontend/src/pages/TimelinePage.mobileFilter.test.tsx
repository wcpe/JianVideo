import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import TimelinePage from './TimelinePage';
import { server } from '@/mocks/beforeAll';

// 移动端筛选折叠（FR-86）：搜索框常驻、筛选控件收进「筛选」抽屉。
// jsdom 下 matchMedia.matches=false，hiddenFrom/visibleFrom 仅做 CSS 显隐、元素不卸载；
// 抽屉 Drawer 关闭时其内容不渲染到 DOM，故未打开前文档中无重复筛选控件。
// 本测试以「触发器存在 + 抽屉内控件可达 + 调整带参 + 收起保留」断言，不依赖真实视口尺寸。

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return { ...actual, useNavigate: () => vi.fn() };
});

vi.mock('@mantine/notifications', () => ({
  notifications: { show: vi.fn() },
}));

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/']}>
        <TimelinePage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('TimelinePage 移动端筛选折叠（FR-86）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 }),
      ),
      http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })),
    );
  });

  it('提供「筛选」抽屉触发器，搜索框常驻在抽屉之外', async () => {
    renderPage();

    // 搜索框常驻（不收进抽屉）
    expect(await screen.findByPlaceholderText(/搜索：文件名/)).toBeInTheDocument();
    // 移动端「筛选」触发按钮存在
    expect(screen.getByRole('button', { name: '筛选' })).toBeInTheDocument();
  });

  it('默认抽屉关闭：抽屉内的「仅收藏」控件未渲染', async () => {
    renderPage();
    await screen.findByPlaceholderText(/搜索：文件名/);

    // 抽屉未打开时（桌面内联区在 jsdom 中以 visibleFrom 隐藏但元素存在，
    // 仍可查到一份内联控件；抽屉那份不渲染）——断言不存在重复弹层 dialog
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('点「筛选」展开抽屉后，抽屉内调整筛选项仍带进请求', async () => {
    const favoriteParams: (string | null)[] = [];
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        favoriteParams.push(new URL(request.url).searchParams.get('favorite'));
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 });
      }),
      http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })),
    );

    const user = userEvent.setup();
    renderPage();
    await waitFor(() => expect(favoriteParams.length).toBeGreaterThan(0));

    // 打开筛选抽屉
    await user.click(screen.getByRole('button', { name: '筛选' }));
    const dialog = await screen.findByRole('dialog');

    // 抽屉内切换「仅收藏」→ 请求带 favorite=true
    await user.click(within(dialog).getByRole('switch', { name: '仅收藏' }));
    await waitFor(() => expect(favoriteParams).toContain('true'));
  });

  it('收起抽屉后再次打开，已选筛选项保留', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByPlaceholderText(/搜索：文件名/);

    // 打开 → 勾选「仅收藏」
    await user.click(screen.getByRole('button', { name: '筛选' }));
    let dialog = await screen.findByRole('dialog');
    const toggle = within(dialog).getByRole('switch', { name: '仅收藏' });
    await user.click(toggle);
    await waitFor(() => expect(toggle).toBeChecked());

    // 关闭抽屉（点抽屉关闭按钮，aria-label 显式设为「关闭筛选」）
    await user.click(within(dialog).getByRole('button', { name: '关闭筛选' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    // 再次打开 → 「仅收藏」仍为已选（状态由页面持有，未被抽屉开合重置）
    await user.click(screen.getByRole('button', { name: '筛选' }));
    dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('switch', { name: '仅收藏' })).toBeChecked();
  });
});
