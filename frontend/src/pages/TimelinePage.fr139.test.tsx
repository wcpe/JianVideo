import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import TimelinePage from './TimelinePage';
import { server } from '@/mocks/beforeAll';

// 时间轴工具栏冻结吸顶与分类筛选（FR-139）：
// 搜索 + 内容筛选 + 视图控件聚合为顶部工具栏，滚动时 sticky 冻结吸顶；
// 内容筛选以「分类」入口承载（复用标签 FR-41，内置映射 favorite 的收藏夹）。

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

describe('TimelinePage 工具栏冻结吸顶与分类筛选（FR-139）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 }),
      ),
      http.get('*/api/library/tags', () =>
        HttpResponse.json({
          items: [{ id: 7, name: '精选', created_at: '2025-01-01T00:00:00Z' }],
        }),
      ),
    );
  });

  it('工具栏容器 sticky 冻结吸顶', async () => {
    renderPage();
    const toolbar = await screen.findByTestId('timeline-toolbar');
    expect(toolbar).toBeInTheDocument();
    expect(getComputedStyle(toolbar).position).toBe('sticky');
  });

  it('分类筛选选择「收藏夹」后媒体请求带 favorite=true', async () => {
    const favoriteParams: (string | null)[] = [];
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        favoriteParams.push(new URL(request.url).searchParams.get('favorite'));
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 });
      }),
    );
    renderPage();
    await waitFor(() => expect(favoriteParams.length).toBeGreaterThan(0));

    const select = await screen.findByRole('combobox', { name: '分类' });
    await userEvent.selectOptions(select, 'favorite');
    await waitFor(() => expect(favoriteParams).toContain('true'));
  });

  it('分类筛选选择标签后媒体请求带 tag_id', async () => {
    const tagParams: (string | null)[] = [];
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        tagParams.push(new URL(request.url).searchParams.get('tag_id'));
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 });
      }),
    );
    renderPage();
    await waitFor(() => expect(tagParams.length).toBeGreaterThan(0));

    const select = await screen.findByRole('combobox', { name: '分类' });
    await waitFor(() => expect(screen.getByRole('option', { name: '精选' })).toBeInTheDocument());
    await userEvent.selectOptions(select, 'tag:7');
    await waitFor(() => expect(tagParams).toContain('7'));
  });
});
