import { HttpResponse, http } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '../mocks/beforeAll';
import client from './client';
import { cleanStorageCache, getStorageCacheSummary, inventoryStorageCache } from './storage-cache';
import { getTask, listTasks } from './tasks';

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

  it('默认 mock 以 202 返回盘点任务并可查询、轮询同一任务成功', async () => {
    const response = await client.post<{ task_id: number }>('/api/storage/cache/inventory');
    const taskID = String(response.data.task_id);

    expect(response.status).toBe(202);
    const runningPage = await listTasks({ type: 'cache.inventory', page_size: 100 });
    expect(runningPage.items.find((task) => task.id === taskID)).toMatchObject({
      id: taskID,
      type: 'cache.inventory',
      status: 'running',
    });
    const completedPage = await listTasks({ type: 'cache.inventory', page_size: 100 });
    expect(completedPage.items.find((task) => task.id === taskID)).toMatchObject({
      id: taskID,
      status: 'succeeded',
    });
    await expect(getTask(taskID)).resolves.toMatchObject({ id: taskID, status: 'succeeded' });
  });

  it('默认 mock 以 202 返回真实清理任务并可用同一 ID 查询成功', async () => {
    const response = await client.post<{ task_id: number }>('/api/storage/cache/clean', {
      dry_run: false,
      kinds: ['thumbnail'],
    });
    const taskID = String(response.data.task_id);

    expect(response.status).toBe(202);
    expect(response.data.task_id).toEqual(expect.any(Number));
    await expect(getTask(taskID)).resolves.toMatchObject({
      id: taskID,
      type: 'cache.clean',
      status: 'succeeded',
    });
  });

  it('触发盘点与 dry-run 清理', async () => {
    const seen: string[] = [];
    server.use(
      http.post('*/api/storage/cache/inventory', () =>
        HttpResponse.json({ task_id: 10 }, { status: 202 }),
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

    await expect(inventoryStorageCache()).resolves.toEqual({ task_id: 10 });
    await expect(cleanStorageCache({ dry_run: true, kinds: ['thumbnail'] })).resolves.toMatchObject(
      {
        dry_run: true,
        candidate_count: 1,
      },
    );
    expect(seen).toEqual(['true:thumbnail']);
  });
});
