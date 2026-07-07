import { describe, expect, it } from 'vitest';
import { createMockFetch, findScenario, handleMockApiRequest } from './index';

describe('mock package', () => {
  it('能解析百万素材场景', () => {
    expect(findScenario('million-assets').dataset).toBe('target-1m');
  });

  it('未知场景抛出中文错误', () => {
    expect(() => findScenario('ai-review-pending')).toThrow('未知 mock 场景');
  });

  it('按 Space 返回媒体分页', async () => {
    const fetch = createMockFetch();
    const response = await fetch('https://mock.local/api/v2/media?page=1&page_size=1', {
      headers: { 'X-JianVideo-Space-Id': 'space-studio' },
    });
    const body = (await response.json()) as { readonly items: readonly [{ readonly space_id: string }]; readonly total: number };

    expect(response.status).toBe(200);
    expect(body.total).toBe(1);
    expect(body.items[0].space_id).toBe('space-studio');
  });

  it('媒体详情遵守 Space 隔离', async () => {
    const response = handleMockApiRequest(
      new Request('https://mock.local/api/v2/media/media-family-001', {
        headers: { 'X-JianVideo-Space-Id': 'space-studio' },
      }),
    );
    const body = (await response.json()) as { readonly code: string };

    expect(response.status).toBe(404);
    expect(body.code).toBe('MEDIA_NOT_FOUND');
  });

  it('任务轮询从 running 进入 succeeded', async () => {
    const fetch = createMockFetch();

    const first = await fetch('https://mock.local/api/v2/tasks/task-transcode-default', {
      headers: { 'X-JianVideo-Space-Id': 'space-default' },
    });
    const second = await fetch('https://mock.local/api/v2/tasks/task-transcode-default', {
      headers: { 'X-JianVideo-Space-Id': 'space-default' },
    });
    const firstBody = (await first.json()) as { readonly status: string; readonly progress: number };
    const secondBody = (await second.json()) as { readonly status: string; readonly progress: number };

    expect(firstBody).toMatchObject({ progress: 0.5, status: 'running' });
    expect(secondBody).toMatchObject({ progress: 1, status: 'succeeded' });
  });

  it('保留旧任务状态响应供 client 兼容映射', async () => {
    const response = handleMockApiRequest(
      new Request('https://mock.local/api/v2/tasks/task-legacy-completed', {
        headers: { 'X-JianVideo-Space-Id': 'space-default' },
      }),
    );
    const body = (await response.json()) as { readonly status: string };

    expect(body.status).toBe('completed');
  });

  it('默认分页参数可回退', async () => {
    const response = await createMockFetch()(new URL('https://mock.local/api/v2/media'), {
      headers: { 'X-JianVideo-Space-Id': 'space-default' },
    });
    const body = (await response.json()) as { readonly page: number; readonly page_size: number };

    expect(body).toMatchObject({ page: 1, page_size: 20 });
  });

  it('未知任务和未知路径返回中文错误', async () => {
    const missingTask = handleMockApiRequest(
      new Request('https://mock.local/api/v2/tasks/missing', {
        headers: { 'X-JianVideo-Space-Id': 'space-default' },
      }),
    );
    const missingPath = handleMockApiRequest(new Request('https://mock.local/api/v2/unknown'));
    const taskBody = (await missingTask.json()) as { readonly code: string; readonly message: string };
    const pathBody = (await missingPath.json()) as { readonly code: string; readonly message: string };

    expect(missingTask.status).toBe(404);
    expect(taskBody).toEqual({ code: 'TASK_NOT_FOUND', message: '任务不存在' });
    expect(missingPath.status).toBe(404);
    expect(pathBody).toEqual({ code: 'MOCK_NOT_FOUND', message: 'mock 接口不存在' });
  });

  it('fetch 可接收 Request 输入并沿用其 headers', async () => {
    const response = await createMockFetch()(
      new Request('https://mock.local/api/v2/media/media-studio-001', {
        headers: { 'X-JianVideo-Space-Id': 'space-studio' },
      }),
    );
    const body = (await response.json()) as { readonly id: string };

    expect(body.id).toBe('media-studio-001');
  });
});
