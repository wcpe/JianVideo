import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import RecyclePage from './RecyclePage';
import { server } from '@/mocks/beforeAll';

const mockNotificationShow = vi.fn();

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}));

function makeMedia(id: number, name: string, deletedAt?: string) {
  return {
    id,
    library_id: 1,
    file_path: `D:\\A\\${name}`,
    file_name: name,
    file_size: 100,
    format: 'mkv',
    video_codec: 'h264',
    audio_codec: 'aac',
    duration: 100,
    width: 1920,
    height: 1080,
    bitrate: 5000000,
    subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z',
    modified_at: '2025-01-01T00:00:00Z',
    // 软删时间（FR2-054）：供到期提示；缺省时行内可不展示具体日期
    deleted_at: deletedAt ?? '2026-07-01T00:00:00Z',
  };
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/recycle']}>
        <RecyclePage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('RecyclePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('渲染回收站页标题', async () => {
    server.use(http.get('*/api/library/recycle', () => HttpResponse.json({ items: [] })));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('回收站')).toBeVisible();
    });
  });

  it('空回收站显示提示文案', async () => {
    server.use(http.get('*/api/library/recycle', () => HttpResponse.json({ items: [] })));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/回收站是空的/)).toBeVisible();
    });
  });

  it('列出已软删的媒体', async () => {
    server.use(
      http.get('*/api/library/recycle', () =>
        HttpResponse.json({
          items: [makeMedia(1, '误删的片子.mkv')],
        }),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('误删的片子.mkv')).toBeVisible();
    });
  });

  it('点击还原调用还原接口并从列表移除', async () => {
    let restoredID: string | null = null;
    server.use(
      http.get('*/api/library/recycle', () =>
        HttpResponse.json({
          items: [makeMedia(7, '待还原.mkv')],
        }),
      ),
      http.post('*/api/library/media/:id/restore', ({ params }) => {
        restoredID = String(params.id);
        return new HttpResponse(null, { status: 204 });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    const card = (await screen.findByText('待还原.mkv')).closest(
      '.mantine-Card-root',
    ) as HTMLElement;
    await user.click(within(card).getByRole('button', { name: '还原' }));

    await waitFor(() => {
      expect(restoredID).toBe('7');
    });
  });

  it('媒体行用统一 MediaRow 渲染（缩略图 + 文件名 + 还原，FR-101）', async () => {
    server.use(
      http.get('*/api/library/recycle', () =>
        HttpResponse.json({
          items: [makeMedia(1, '统一行.mkv')],
        }),
      ),
    );
    renderPage();
    const card = (await screen.findByText('统一行.mkv')).closest(
      '.mantine-Card-root',
    ) as HTMLElement;
    // MediaRow 复用 MediaThumbnail：行内含缩略图 img（alt=文件名）
    expect(within(card).getByRole('img', { name: '统一行.mkv' })).toBeInTheDocument();
    // 行内含还原操作
    expect(within(card).getByRole('button', { name: '还原' })).toBeVisible();
  });

  // ─── 回收站清理（FR-26）──────────────────────────────

  it('有软删项时展示清理回收站按钮', async () => {
    server.use(
      http.get('*/api/library/recycle', () =>
        HttpResponse.json({
          items: [makeMedia(3, '要清的.mkv')],
        }),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /清理回收站/ })).toBeVisible();
    });
  });

  it('点击清理弹二次确认，确认后调用清理接口并清空列表', async () => {
    let cleaned = false;
    server.use(
      http.get('*/api/library/recycle', () =>
        HttpResponse.json({
          items: cleaned ? [] : [makeMedia(5, '待清理.mkv')],
        }),
      ),
      http.post('*/api/library/recycle/cleanup', () => {
        cleaned = true;
        return HttpResponse.json({ moved: 1, failed: 0 });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole('button', { name: /清理回收站/ }));
    // 二次确认弹窗里的确认按钮
    await user.click(await screen.findByRole('button', { name: '清理' }));

    await waitFor(() => {
      expect(cleaned).toBe(true);
    });
    await waitFor(() => {
      expect(screen.getByText(/回收站是空的/)).toBeVisible();
    });
  });

  it('未配置回收站路径（409）时提示去设置页配置', async () => {
    server.use(
      http.get('*/api/library/recycle', () =>
        HttpResponse.json({
          items: [makeMedia(9, '没配路径.mkv')],
        }),
      ),
      http.post('*/api/library/recycle/cleanup', () =>
        HttpResponse.json(
          { code: 'RECYCLE_PATH_UNSET', message: '以下盘符未配置回收站路径: D' },
          { status: 409 },
        ),
      ),
    );

    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole('button', { name: /清理回收站/ }));
    await user.click(await screen.findByRole('button', { name: '清理' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalled();
    });
    const msgArg = mockNotificationShow.mock.calls.map((c) => JSON.stringify(c[0])).join(' ');
    expect(msgArg).toMatch(/设置/);
  });

  // ─── 保留期提示（FR2-054）──────────────────────────────

  it('展示保留策略摘要与行内预计清理日期', async () => {
    server.use(
      http.get('*/api/library/recycle', () =>
        HttpResponse.json({
          items: [makeMedia(11, '将到期.mkv', '2026-07-10T00:00:00')],
        }),
      ),
      http.get('*/api/settings', () =>
        HttpResponse.json({
          settings: {
            recycle_retention_days: '30',
            recycle_auto_cleanup_enabled: '1',
          },
        }),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/当前保留 30 天/)).toBeVisible();
    });
    // 2026-07-10 + 30 天 → 2026-08-09
    expect(await screen.findByText(/预计 2026-08-09 自动清理/)).toBeVisible();
  });

  it('自动清理关闭时展示仅可手动清理', async () => {
    server.use(
      http.get('*/api/library/recycle', () =>
        HttpResponse.json({
          items: [makeMedia(12, '仅手动.mkv')],
        }),
      ),
      http.get('*/api/settings', () =>
        HttpResponse.json({
          settings: {
            recycle_retention_days: '30',
            recycle_auto_cleanup_enabled: '0',
          },
        }),
      ),
    );
    renderPage();
    // 页顶策略摘要 + 行内副标题均含「仅可手动清理」，分别断言避免多匹配
    await waitFor(() => {
      expect(screen.getByText(/未启用到期自动清理/)).toBeVisible();
    });
    expect(await screen.findByText('自动清理已关闭，仅可手动清理')).toBeVisible();
  });
});
