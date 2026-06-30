import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import TimelinePage from './TimelinePage';
import { server } from '@/mocks/beforeAll';

// FR-144 时间轴媒体库筛选：时间轴选择某图库后仅请求该库媒体（library_id 过滤），
// 「全部图库」恢复全量。复用后端 library_id 过滤（MediaListParams.library_id）。

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

describe('TimelinePage 媒体库筛选（FR-144）', () => {
  // 记录每次 media 请求携带的 library_id 查询参数
  let lastLibraryId: string | null;

  beforeEach(() => {
    vi.clearAllMocks();
    lastLibraryId = null;
    Object.defineProperty(window, 'scrollY', { value: 0, writable: true, configurable: true });
    server.use(
      http.get('*/api/library/paths', () =>
        HttpResponse.json({
          items: [
            { id: 1, path: 'D:\\Videos\\Movies', type: 'local', label: '电影', enabled: true, created_at: '2025-01-01T00:00:00Z' },
            { id: 2, path: 'D:\\Videos\\TV', type: 'local', label: '电视剧', enabled: true, created_at: '2025-01-02T00:00:00Z' },
          ],
        }),
      ),
      http.get('*/api/library/extensions', () => HttpResponse.json({ items: [] })),
      http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })),
      http.get('*/api/library/media', ({ request }) => {
        lastLibraryId = new URL(request.url).searchParams.get('library_id');
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 60 });
      }),
    );
  });

  it('选择某图库后媒体请求携带对应 library_id', async () => {
    const user = userEvent.setup();
    renderPage();
    // 桌面端筛选区内联（默认非窄屏），等图库下拉出现
    const select = await screen.findByRole('combobox', { name: '图库' });
    await waitFor(() => expect(screen.getByRole('option', { name: '电视剧' })).toBeInTheDocument());

    await user.selectOptions(select, '2');

    await waitFor(() => expect(lastLibraryId).toBe('2'));
  });

  it('选回「全部图库」后媒体请求不带 library_id（恢复全量）', async () => {
    const user = userEvent.setup();
    renderPage();
    const select = await screen.findByRole('combobox', { name: '图库' });
    await waitFor(() => expect(screen.getByRole('option', { name: '电影' })).toBeInTheDocument());

    await user.selectOptions(select, '1');
    await waitFor(() => expect(lastLibraryId).toBe('1'));

    await user.selectOptions(select, '0');
    // 全部图库 → library_id 不下发
    await waitFor(() => expect(lastLibraryId).toBeNull());
  });
});
