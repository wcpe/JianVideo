import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import { backfillMediaInferences } from './library';
import { getTask } from './tasks';

describe('影视信息回填 API', () => {
  it('返回 202 接受任务契约并保留 task_id', async () => {
    let payload: unknown = null;
    server.use(
      http.post('*/api/library/inference/backfill', async ({ request }) => {
        payload = await request.json();
        return HttpResponse.json({ status: 'pending', task_id: 42 }, { status: 202 });
      }),
    );

    const accepted = await backfillMediaInferences(7);

    expect(payload).toEqual({ library_id: 7 });
    expect(accepted).toEqual({ status: 'pending', task_id: 42 });
  });

  it('mock 任务详情可轮询到 succeeded 终态', async () => {
    const accepted = await backfillMediaInferences(7);

    await vi.waitFor(
      async () => {
        const task = await getTask(String(accepted.task_id));
        expect(task.status).toBe('succeeded');
        expect(task.progress).toBe(1);
      },
      { timeout: 2000 },
    );
  });
});
