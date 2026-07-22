import { render, screen, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import TimelinePage from './TimelinePage';
import { server } from '@/mocks/beforeAll';

// 时间轴筛选分组（FR-100）：把筛选控件按「内容筛选 | 视图」分组成带标签容器，
// 既有受控控件（收藏/标签/类型/大小/日期/缩放）仍在各自分组内可达、请求参数不变。

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

describe('TimelinePage 筛选分组（FR-100）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 }),
      ),
      http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })),
    );
  });

  it('筛选区按「内容筛选 / 视图」两组带标签容器渲染', async () => {
    renderPage();

    // 两个分组容器（role=group）均存在
    const contentGroup = await screen.findByRole('group', { name: '内容筛选' });
    const viewGroup = await screen.findByRole('group', { name: '视图' });
    expect(contentGroup).toBeInTheDocument();
    expect(viewGroup).toBeInTheDocument();

    // 内容筛选组内含分类筛选（FR-139）与类型筛选；视图组内含缩放
    expect(within(contentGroup).getByRole('combobox', { name: '分类' })).toBeInTheDocument();
    expect(within(contentGroup).getByRole('radiogroup', { name: '类型筛选' })).toBeInTheDocument();
    expect(within(viewGroup).getByRole('radiogroup', { name: '时间轴缩放' })).toBeInTheDocument();
  });

  it('分组容器内调整筛选项仍带进请求（受控逻辑不回归）', async () => {
    const typeParams: (string | null)[] = [];
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        typeParams.push(new URL(request.url).searchParams.get('type'));
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 });
      }),
      http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })),
    );

    const { default: userEvent } = await import('@testing-library/user-event');
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => expect(typeParams.length).toBeGreaterThan(0));

    const contentGroup = await screen.findByRole('group', { name: '内容筛选' });
    await user.click(within(contentGroup).getByRole('radio', { name: '图片' }));
    await waitFor(() => expect(typeParams).toContain('image'));
  });
});
