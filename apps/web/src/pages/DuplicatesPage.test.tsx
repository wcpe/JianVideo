import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import DuplicatesPage from './DuplicatesPage';
import { server } from '@/mocks/beforeAll';

const mockNotificationShow = vi.fn();

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}));

function makeMedia(id: number, name: string) {
  return {
    id,
    library_id: 1,
    file_path: `D:\\A\\${name}`,
    file_name: name,
    file_size: 100,
    format: 'jpg',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 1920,
    height: 1080,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z',
    modified_at: '2025-01-01T00:00:00Z',
  };
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/duplicates']}>
        <DuplicatesPage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('DuplicatesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.use(http.get('*/api/library/duplicates/exact', () => HttpResponse.json({ groups: [] })));
  });

  it('渲染页面标题', async () => {
    server.use(http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })));
    renderPage();
    await waitFor(() => expect(screen.getByText('重复项')).toBeVisible());
  });

  it('无重复组时显示空态提示', async () => {
    server.use(http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })));
    renderPage();
    await waitFor(() => expect(screen.getByText(/没有发现重复/)).toBeVisible());
  });

  it('空态展示引导文案与可点扫描 CTA', async () => {
    let scanned = false;
    server.use(
      http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })),
      http.post('*/api/library/duplicates/scan', () => {
        scanned = true;
        return HttpResponse.json({ computed: 0 });
      }),
    );
    const user = userEvent.setup();
    renderPage();
    expect(await screen.findByText('没有发现重复项')).toBeVisible();
    await user.click(screen.getByRole('tab', { name: '相似重复' }));
    const emptyState = screen.getByTestId('empty-state');
    await user.click(within(emptyState).getByRole('button', { name: /扫描相似重复/ }));
    await waitFor(() => expect(scanned).toBe(true));
  });

  it('列出重复组成员', async () => {
    server.use(
      http.get('*/api/library/duplicates', () =>
        HttpResponse.json({
          groups: [[makeMedia(1, '重复A.jpg'), makeMedia(2, '重复B.jpg')]],
        }),
      ),
    );
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('tab', { name: '相似重复' }));
    await waitFor(() => {
      expect(screen.getByText('重复A.jpg')).toBeVisible();
      expect(screen.getByText('重复B.jpg')).toBeVisible();
    });
  });

  it('点击扫描重复项调用扫描接口并刷新', async () => {
    let scanned = false;
    server.use(
      http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })),
      http.post('*/api/library/duplicates/scan', () => {
        scanned = true;
        return HttpResponse.json({ computed: 0 });
      }),
    );
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('tab', { name: '相似重复' }));
    await user.click(await screen.findByRole('button', { name: /扫描相似重复/ }));
    await waitFor(() => expect(scanned).toBe(true));
  });

  it('选中成员后批量删除调用批量软删端点并刷新', async () => {
    let deletedIDs: number[] = [];
    let listCalls = 0;
    server.use(
      http.get('*/api/library/duplicates', () => {
        listCalls++;
        // 第一次返回一组两项；删除后第二次返回空（已清理）
        if (listCalls === 1) {
          return HttpResponse.json({
            groups: [[makeMedia(11, '保留.jpg'), makeMedia(12, '多余.jpg')]],
          });
        }
        return HttpResponse.json({ groups: [] });
      }),
      http.post('*/api/library/media/batch-delete', async ({ request }) => {
        const body = (await request.json()) as { ids: number[] };
        deletedIDs = body.ids;
        return HttpResponse.json({ deleted: body.ids.length });
      }),
    );

    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('tab', { name: '相似重复' }));

    const card = (await screen.findByText('多余.jpg')).closest('.mantine-Card-root') as HTMLElement;
    await user.click(within(card).getByRole('checkbox'));

    await user.click(screen.getByRole('button', { name: /删除选中项/ }));

    await waitFor(() => expect(deletedIDs).toEqual([12]));
    // 删除后列表刷新为空态
    await waitFor(() => expect(screen.getByText(/没有发现重复/)).toBeVisible());
  });

  it('媒体行用统一 MediaRow 渲染（缩略图 + 勾选 + 文件名，FR-101）', async () => {
    server.use(
      http.get('*/api/library/duplicates', () =>
        HttpResponse.json({
          groups: [[makeMedia(1, '统一行A.jpg'), makeMedia(2, '统一行B.jpg')]],
        }),
      ),
    );
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('tab', { name: '相似重复' }));
    const card = (await screen.findByText('统一行A.jpg')).closest(
      '.mantine-Card-root',
    ) as HTMLElement;
    expect(within(card).getByRole('img', { name: '统一行A.jpg' })).toBeInTheDocument();
    expect(within(card).getByRole('checkbox', { name: '选择 统一行A.jpg' })).toBeInTheDocument();
  });

  it('未选中任何项时删除按钮禁用', async () => {
    server.use(
      http.get('*/api/library/duplicates', () =>
        HttpResponse.json({
          groups: [[makeMedia(1, 'a.jpg'), makeMedia(2, 'b.jpg')]],
        }),
      ),
    );
    renderPage();
    const btn = await screen.findByRole('button', { name: /删除选中项/ });
    expect(btn).toBeDisabled();
  });

  it('区分精确重复与相似重复', async () => {
    server.use(
      http.get('*/api/library/duplicates/exact', () =>
        HttpResponse.json({
          groups: [
            {
              content_hash: 'hash-a',
              file_size: 100,
              items: [makeMedia(21, '精确A.mp4'), makeMedia(22, '精确B.mp4')],
            },
          ],
        }),
      ),
      http.get('*/api/library/duplicates', () =>
        HttpResponse.json({
          groups: [[makeMedia(31, '相似A.jpg'), makeMedia(32, '相似B.jpg')]],
        }),
      ),
    );
    const user = userEvent.setup();
    renderPage();

    expect(await screen.findByRole('tab', { name: '精确重复' })).toBeVisible();
    expect(screen.getByRole('tab', { name: '相似重复' })).toBeVisible();
    expect(await screen.findByText('精确A.mp4')).toBeVisible();
    expect(screen.queryByText('相似A.jpg')).not.toBeInTheDocument();

    await user.click(screen.getByRole('tab', { name: '相似重复' }));
    expect(await screen.findByText('相似A.jpg')).toBeVisible();
  });

  it('AI 相似 Tab 列出候选组并展示相似度（FR2-012）', async () => {
    server.use(
      http.get('*/api/library/duplicates/exact', () => HttpResponse.json({ groups: [] })),
      http.get('*/api/ai/duplicates', () =>
        HttpResponse.json({
          items: [{ media_ids: [41, 42], score: 0.97, model_id: 'stub-embed-v1' }],
        }),
      ),
      http.get('*/api/library/media/41', () => HttpResponse.json(makeMedia(41, 'AI候选A.jpg'))),
      http.get('*/api/library/media/42', () => HttpResponse.json(makeMedia(42, 'AI候选B.jpg'))),
    );
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('tab', { name: 'AI 相似' }));
    expect(await screen.findByText('AI候选A.jpg')).toBeVisible();
    expect(screen.getByText('AI候选B.jpg')).toBeVisible();
    expect(screen.getByText(/相似度 97/)).toBeVisible();
  });

  it('AI 未启用时展示关闭提示（FR2-012）', async () => {
    server.use(
      http.get('*/api/library/duplicates/exact', () => HttpResponse.json({ groups: [] })),
      http.get('*/api/ai/duplicates', () =>
        HttpResponse.json({ code: 'AI_DISABLED', message: 'AI 能力未启用' }, { status: 503 }),
      ),
    );
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole('tab', { name: 'AI 相似' }));
    expect(await screen.findByText(/AI 未启用/)).toBeVisible();
  });

  it('点击回填精确哈希调用任务端点并保留相似扫描入口', async () => {
    let backfillCalled = false;
    let similarScanCalled = false;
    server.use(
      http.get('*/api/library/duplicates/exact', () => HttpResponse.json({ groups: [] })),
      http.get('*/api/library/duplicates', () => HttpResponse.json({ groups: [] })),
      http.post('*/api/library/file-hashes/backfill', () => {
        backfillCalled = true;
        return HttpResponse.json({ status: 'queued', task_id: '7' }, { status: 202 });
      }),
      http.post('*/api/library/duplicates/scan', () => {
        similarScanCalled = true;
        return HttpResponse.json({ computed: 0 });
      }),
    );
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole('button', { name: /回填精确哈希/ }));
    await waitFor(() => expect(backfillCalled).toBe(true));

    await user.click(screen.getByRole('tab', { name: '相似重复' }));
    await user.click(await screen.findByRole('button', { name: /扫描相似重复/ }));
    await waitFor(() => expect(similarScanCalled).toBe(true));
  });
});
