import { describe, expect, it } from 'vitest';
import {
  ApiError,
  createApiClient,
  createQueryKeys,
  getMedia,
  getTask,
  detectDeviceCapabilities,
  listMedia,
  normalizeLegacyTaskState,
  taskPollInterval,
  type FetchLike,
} from './index';

describe('media-client package', () => {
  it('query key 包含 Space 维度', () => {
    expect(createQueryKeys().mediaList({ spaceId: 'default' }, 2)).toEqual(['media', 'list', 'default', 2]);
  });

  it('兼容旧任务状态并映射到 ADR-0055 状态', () => {
    expect(normalizeLegacyTaskState('completed')).toBe('succeeded');
    expect(normalizeLegacyTaskState('error')).toBe('failed');
  });

  it('保留 ADR-0055 原生任务状态', () => {
    expect(normalizeLegacyTaskState('pending')).toBe('pending');
    expect(normalizeLegacyTaskState('running')).toBe('running');
    expect(normalizeLegacyTaskState('succeeded')).toBe('succeeded');
    expect(normalizeLegacyTaskState('failed')).toBe('failed');
    expect(normalizeLegacyTaskState('canceled')).toBe('canceled');
  });

  it('未知任务状态抛出中文错误', () => {
    expect(() => normalizeLegacyTaskState('paused')).toThrow('未知任务状态');
  });

  it('请求携带 Space 上下文和鉴权头', async () => {
    const requests: Request[] = [];
    const client = createApiClient({
      authToken: 'token-a',
      baseUrl: 'https://mock.local',
      fetch: (input, init) => {
        const request = new Request(input, init);
        requests.push(request);
        return Promise.resolve(Response.json({ ok: true }));
      },
      space: { spaceId: 'space-a' },
    });

    await client.request('/api/v2/ping');

    expect(requests).toHaveLength(1);
    expect(requests[0]?.headers.get('Authorization')).toBe('Bearer token-a');
    expect(requests[0]?.headers.get('X-JianVideo-Space-Id')).toBe('space-a');
  });

  it('规范化接口错误', async () => {
    const client = createApiClient({
      fetch: () =>
        Promise.resolve(Response.json({ code: 'SPACE_FORBIDDEN', message: '无权访问此 Space' }, { status: 403 })),
      space: { spaceId: 'space-a' },
    });

    await expect(client.request('/api/v2/media')).rejects.toMatchObject({
      code: 'SPACE_FORBIDDEN',
      message: '无权访问此 Space',
      status: 403,
    });
  });

  it('按 Space 和分页生成稳定 query key', () => {
    const keys = createQueryKeys();

    expect(keys.mediaList({ spaceId: 'space-a' }, { page: 1, pageSize: 2 })).toEqual([
      'media',
      'list',
      'space-a',
      { page: 1, pageSize: 2 },
    ]);
    expect(keys.mediaList({ spaceId: 'space-b' }, { page: 1, pageSize: 2 })).not.toEqual(
      keys.mediaList({ spaceId: 'space-a' }, { page: 1, pageSize: 2 }),
    );
    expect(keys.taskDetail({ spaceId: 'space-a' }, 'task-1')).toEqual(['tasks', 'detail', 'space-a', 'task-1']);
  });

  it('对 mock fetch 跑通媒体分页、详情和任务轮询', async () => {
    const { createMockFetch } = (await import(new URL('../../mock/src/index.ts', import.meta.url).href)) as {
      readonly createMockFetch: () => FetchLike;
    };
    const client = createApiClient({ fetch: createMockFetch(), space: { spaceId: 'space-default' } });

    const page = await listMedia(client, { page: 1, pageSize: 1 });
    const detail = await getMedia(client, page.items[0]?.id ?? '');
    const runningTask = await getTask(client, 'task-transcode-default');
    const finishedTask = await getTask(client, 'task-transcode-default');

    expect(page.items).toHaveLength(1);
    expect(page.total).toBeGreaterThan(1);
    expect(detail.spaceId).toBe('space-default');
    expect(runningTask.status).toBe('running');
    expect(finishedTask.status).toBe('succeeded');
    expect(taskPollInterval(runningTask)).toBe(2_000);
    expect(taskPollInterval(finishedTask)).toBe(false);
  });

  it('切换 Space 后读取不同媒体列表', async () => {
    const { createMockFetch } = (await import(new URL('../../mock/src/index.ts', import.meta.url).href)) as {
      readonly createMockFetch: () => FetchLike;
    };
    const mockFetch = createMockFetch();
    const defaultClient = createApiClient({ fetch: mockFetch, space: { spaceId: 'space-default' } });
    const studioClient = defaultClient.withSpace({ spaceId: 'space-studio' });

    const defaultPage = await listMedia(defaultClient, { page: 1, pageSize: 10 });
    const studioPage = await listMedia(studioClient, { page: 1, pageSize: 10 });

    expect(defaultPage.items.map((item) => item.spaceId)).toEqual(['space-default', 'space-default']);
    expect(studioPage.items.map((item) => item.spaceId)).toEqual(['space-studio']);
  });

  it('网络错误会转成 ApiError', async () => {
    const client = createApiClient({
      fetch: () => Promise.reject(new TypeError('failed')),
      space: { spaceId: 'space-default' },
    });

    await expect(client.request('/api/v2/media')).rejects.toBeInstanceOf(ApiError);
    await expect(client.request('/api/v2/media')).rejects.toMatchObject({ code: 'NETWORK_ERROR', status: 0 });
  });

  it('可配置重试策略并在临时失败后成功', async () => {
    let attempts = 0;
    const client = createApiClient({
      fetch: () => {
        attempts += 1;
        if (attempts === 1) {
          return Promise.resolve(Response.json({ code: 'SERVER_BUSY', message: '服务繁忙' }, { status: 503 }));
        }
        return Promise.resolve(Response.json({ ok: true }));
      },
      retry: { attempts: 2 },
      space: { spaceId: 'space-default' },
    });

    await expect(client.request('/api/v2/retry')).resolves.toEqual({ ok: true });
    expect(attempts).toBe(2);
  });

  it('客户端错误不会触发重试', async () => {
    let attempts = 0;
    const client = createApiClient({
      fetch: () => {
        attempts += 1;
        return Promise.resolve(Response.json({ code: 'BAD_REQUEST', message: '请求无效' }, { status: 400 }));
      },
      retry: { attempts: 3 },
      space: { spaceId: 'space-default' },
    });

    await expect(client.request('/api/v2/bad-request')).rejects.toMatchObject({ code: 'BAD_REQUEST', status: 400 });
    expect(attempts).toBe(1);
  });

  it('检测 Web、Desktop、Mobile、TV、车机、触控和网络能力', () => {
    const web = detectDeviceCapabilities({ navigator: { onLine: true, userAgent: 'Mozilla/5.0' } });
    const desktop = detectDeviceCapabilities({ navigator: { userAgent: 'Mozilla/5.0 (Windows NT 10.0)' } });
    const mobile = detectDeviceCapabilities({
      matchMedia: (query) => ({ matches: query === '(pointer: coarse)' }),
      navigator: {
        connection: { effectiveType: '2g', saveData: true },
        maxTouchPoints: 5,
        onLine: true,
        userAgent: 'Mozilla/5.0 (Linux; Android 14; Mobile)',
      },
    });
    const tv = detectDeviceCapabilities({ navigator: { userAgent: 'Mozilla/5.0 AppleTV' } });
    const automotive = detectDeviceCapabilities({ navigator: { userAgent: 'Mozilla/5.0 Android Automotive' } });

    expect(web.platform).toBe('web');
    expect(desktop.platform).toBe('desktop');
    expect(mobile).toMatchObject({ network: 'constrained', platform: 'mobile', pointer: 'coarse', touch: true });
    expect(tv.platform).toBe('tv');
    expect(automotive.platform).toBe('automotive');
  });
});
