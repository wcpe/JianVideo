import { MantineProvider } from '@mantine/core';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { HttpResponse, http } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '../mocks/beforeAll';
import StorageCachePage from './StorageCachePage';

describe('StorageCachePage（FR2-048）', () => {
  it('展示缓存统计、轮询盘点任务并执行 dry-run', async () => {
    let cleanCalled = false;
    let summaryCalls = 0;
    server.use(
      http.get('*/api/storage/cache/summary', () => {
        summaryCalls += 1;
        return HttpResponse.json({
          total_size_bytes: 12,
          total_file_count: 3,
          total_assets: 2,
          by_kind: {
            thumbnail: { kind: 'thumbnail', size_bytes: 5, file_count: 1, asset_count: 1 },
            hls: { kind: 'hls', size_bytes: 7, file_count: 2, asset_count: 1 },
            image_proxy: { kind: 'image_proxy', size_bytes: 0, file_count: 0, asset_count: 0 },
            cover: { kind: 'cover', size_bytes: 0, file_count: 0, asset_count: 0 },
            metadata_temp: { kind: 'metadata_temp', size_bytes: 0, file_count: 0, asset_count: 0 },
          },
        });
      }),
      http.post('*/api/storage/cache/inventory', () =>
        HttpResponse.json({ task_id: 10 }, { status: 202 }),
      ),
      http.get('*/api/tasks/10', () =>
        HttpResponse.json({
          id: '10',
          scope: 'space',
          space_id: 'space-default',
          type: 'cache.inventory',
          status: 'succeeded',
          priority: 0,
          attempts: 0,
          max_attempts: 3,
          progress: 1,
          error: null,
          created_at: '2026-07-09T10:00:00Z',
          updated_at: '2026-07-09T10:00:01Z',
        }),
      ),
      http.post('*/api/storage/cache/clean', async ({ request }) => {
        const body = (await request.json()) as { dry_run: boolean };
        cleanCalled = body.dry_run;
        return HttpResponse.json({
          dry_run: true,
          candidate_count: 1,
          total_size_bytes: 5,
          total_file_count: 1,
          deleted_count: 0,
          deleted_size_bytes: 0,
          failed_count: 0,
        });
      }),
    );

    render(
      <MantineProvider>
        <StorageCachePage />
      </MantineProvider>,
    );

    expect(await screen.findByText('缓存管理')).toBeInTheDocument();
    expect(await screen.findAllByText('缩略图')).not.toHaveLength(0);
    expect(screen.getAllByText('HLS')).not.toHaveLength(0);

    await userEvent.click(screen.getByRole('button', { name: '盘点' }));
    await waitFor(() => expect(summaryCalls).toBeGreaterThanOrEqual(2));

    await userEvent.click(screen.getByRole('button', { name: '预览清理' }));

    await waitFor(() => expect(cleanCalled).toBe(true));
    expect(await screen.findByText(/预计影响 1 项/)).toBeInTheDocument();
  });
});
