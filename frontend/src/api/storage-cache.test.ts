import { HttpResponse, http } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '../mocks/beforeAll';
import { cleanStorageCache, getStorageCacheSummary, inventoryStorageCache } from './storage-cache';

describe('storage cache API（FR2-048）', () => {
  it('读取缓存统计', async () => {
    server.use(
      http.get('*/api/storage/cache/summary', () =>
        HttpResponse.json({
          total_size_bytes: 12,
          total_file_count: 3,
          total_assets: 2,
          by_kind: {
            thumbnail: { kind: 'thumbnail', size_bytes: 5, file_count: 1, asset_count: 1 },
            hls: { kind: 'hls', size_bytes: 7, file_count: 2, asset_count: 1 },
          },
        }),
      ),
    );

    const summary = await getStorageCacheSummary();

    expect(summary.total_size_bytes).toBe(12);
    expect(summary.by_kind.hls.file_count).toBe(2);
  });

  it('触发盘点与 dry-run 清理', async () => {
    const seen: string[] = [];
    server.use(
      http.post('*/api/storage/cache/inventory', () =>
        HttpResponse.json({ discovered: 2, missing: 0, task_id: 10 }),
      ),
      http.post('*/api/storage/cache/clean', async ({ request }) => {
        const body = (await request.json()) as { dry_run: boolean; kinds: string[] };
        seen.push(`${body.dry_run}:${body.kinds.join(',')}`);
        return HttpResponse.json({
          dry_run: body.dry_run,
          candidate_count: 1,
          total_size_bytes: 5,
          total_file_count: 1,
          deleted_count: 0,
          deleted_size_bytes: 0,
          failed_count: 0,
        });
      }),
    );

    await expect(inventoryStorageCache()).resolves.toMatchObject({ discovered: 2, task_id: 10 });
    await expect(cleanStorageCache({ dry_run: true, kinds: ['thumbnail'] })).resolves.toMatchObject({
      dry_run: true,
      candidate_count: 1,
    });
    expect(seen).toEqual(['true:thumbnail']);
  });
});
