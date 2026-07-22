import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import {
  createAudioReload,
  createHLSABR,
  getHLSStatus,
  getTimelinePreviewStatus,
  getWatchState,
  negotiate,
  rebuildTimelinePreview,
  updateWatchState,
} from './play';

describe('观看状态 API', () => {
  it('读取 revision 并按 session/event DTO 更新', async () => {
    let body: unknown;
    server.use(
      http.get('*/api/play/9/watch-state', () =>
        HttpResponse.json({
          completed: false,
          created_at: '0001-01-01T00:00:00Z',
          last_event_seq: 0,
          last_session_id: '',
          last_watched_at: '0001-01-01T00:00:00Z',
          media_id: 9,
          position_seconds: 0,
          revision: 0,
          space_id: 'space-default',
          updated_at: '0001-01-01T00:00:00Z',
        }),
      ),
      http.put('*/api/play/9/watch-state', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({
          applied: true,
          current: {
            completed: false,
            created_at: '2026-07-15T09:00:00Z',
            last_event_seq: 2,
            last_session_id: 'session-a',
            last_watched_at: '2026-07-15T10:00:00Z',
            media_id: 9,
            position_seconds: 33,
            revision: 1,
            space_id: 'space-default',
            updated_at: '2026-07-15T10:00:00Z',
          },
        });
      }),
    );

    const current = await getWatchState(9);
    const result = await updateWatchState(9, {
      position_seconds: 33,
      expected_revision: current.revision,
      session_id: 'session-a',
      event_seq: 2,
      event_type: 'progress',
      reason: 'user',
    });

    expect(result).toMatchObject({ applied: true, current: { revision: 1, last_event_seq: 2 } });
    expect(body).toEqual({
      position_seconds: 33,
      expected_revision: 0,
      session_id: 'session-a',
      event_seq: 2,
      event_type: 'progress',
      reason: 'user',
    });
  });

  it('409 响应保留 applied=false 与 current', async () => {
    server.use(
      http.put('*/api/play/9/watch-state', () =>
        HttpResponse.json(
          {
            applied: false,
            code: 'WATCH_STATE_CONFLICT',
            message: '观看状态已被其他会话更新',
            current: {
              completed: false,
              created_at: '2026-07-15T09:00:00Z',
              last_event_seq: 3,
              last_session_id: 'session-b',
              last_watched_at: '2026-07-15T10:01:00Z',
              media_id: 9,
              position_seconds: 45,
              revision: 2,
              space_id: 'space-default',
              updated_at: '2026-07-15T10:01:00Z',
            },
          },
          { status: 409 },
        ),
      ),
    );

    const error = await updateWatchState(9, {
      position_seconds: 80,
      expected_revision: 1,
      session_id: 'session-a',
      event_seq: 3,
      event_type: 'pause',
      reason: 'system',
    }).catch((caught: unknown) => caught);

    expect(error).toMatchObject({
      response: {
        data: {
          applied: false,
          code: 'WATCH_STATE_CONFLICT',
          current: { position_seconds: 45, revision: 2 },
        },
        status: 409,
      },
    });
  });
});

describe('时间轴预览 API', () => {
  it('将 202 作为正常状态并透传查询参数', async () => {
    let requestedProfile = '';
    server.use(
      http.get('*/api/play/9/timeline-preview', ({ request }) => {
        requestedProfile = new URL(request.url).searchParams.get('profile') ?? '';
        return HttpResponse.json(
          { duration: 0, profile_id: 'desktop', status: 'pending', task_id: 42, version: 1 },
          { status: 202 },
        );
      }),
    );

    const status = await getTimelinePreviewStatus(9, 'desktop');

    expect(status).toMatchObject({ profile_id: 'desktop', status: 'pending', task_id: 42 });
    expect(requestedProfile).toBe('desktop');
  });

  it('signal 取消时拒绝状态请求', async () => {
    const controller = new AbortController();
    controller.abort();

    await expect(getTimelinePreviewStatus(9, 'desktop', controller.signal)).rejects.toBeDefined();
  });

  it('把 VTT 与 sprite URL 绝对化', async () => {
    server.use(
      http.get('*/api/play/9/timeline-preview', () =>
        HttpResponse.json({
          duration: 12,
          generation_id: 'generation-a',
          profile_id: 'desktop',
          source_fingerprint: 'source-a',
          sprite_urls: { 'sprite-000.jpg': '/api/play/9/timeline-preview/sprite-000.jpg' },
          status: 'available',
          task_id: 0,
          version: 1,
          vtt_url: '/api/play/9/timeline-preview/index.vtt',
        }),
      ),
    );

    const status = await getTimelinePreviewStatus(9, 'desktop');

    expect(status.vtt_url).toMatch(/^https?:\/\/.+\/api\/play\/9\/timeline-preview\/index\.vtt$/);
    expect(status.sprite_urls?.['sprite-000.jpg']).toMatch(
      /^https?:\/\/.+\/api\/play\/9\/timeline-preview\/sprite-000\.jpg$/,
    );
  });

  it('POST rebuild 并返回正常 202 状态', async () => {
    let body: unknown;
    server.use(
      http.post('*/api/play/9/timeline-preview/rebuild', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(
          {
            duration: 0,
            generation_id: 'generation-new',
            profile_id: 'mobile',
            status: 'pending',
            task_id: 77,
            version: 1,
          },
          { status: 202 },
        );
      }),
    );

    const status = await rebuildTimelinePreview(9, 'mobile');

    expect(body).toEqual({ profile_id: 'mobile' });
    expect(status).toMatchObject({ generation_id: 'generation-new', task_id: 77 });
  });

  it('POST rebuild 未指定 profile 时不传空参数', async () => {
    let body = '未收到请求';
    server.use(
      http.post('*/api/play/9/timeline-preview/rebuild', async ({ request }) => {
        body = await request.text();
        return HttpResponse.json(
          { duration: 0, profile_id: 'timeline-v1', status: 'pending', version: 1 },
          { status: 202 },
        );
      }),
    );

    await rebuildTimelinePreview(9);

    expect(JSON.parse(body)).toEqual({});
  });
});

describe('音轨重载 API', () => {
  it('按修订契约解析 202，并用 task_id 精确查询状态', async () => {
    let body: unknown;
    let statusQuery: URLSearchParams | undefined;
    server.use(
      http.post('*/api/play/9/audio-reload', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(
          {
            task_id: '81',
            profile_id: 'audio-track-b',
            url: '/api/play/hls/9/profiles/audio-track-b/tasks/81/master.m3u8',
            requested_track_id: 'audio-b',
            space_id: 'space-a',
          },
          { status: 202 },
        );
      }),
      http.get('*/api/play/9/hls-status', ({ request }) => {
        statusQuery = new URL(request.url).searchParams;
        return HttpResponse.json({
          available: true,
          profile_id: 'audio-track-b',
          url: '/api/play/hls/9/profiles/audio-track-b/tasks/81/master.m3u8',
          requested_track_id: 'audio-b',
          effective_track_id: 'audio-b',
          task: { id: '81', status: 'succeeded', progress: 100 },
        });
      }),
    );

    const created = await createAudioReload(9, 'audio-b');
    const status = await getHLSStatus(9, created.profile_id, created.task_id);

    expect(body).toEqual({ track_id: 'audio-b' });
    expect(created).toMatchObject({
      task_id: '81',
      requested_track_id: 'audio-b',
      space_id: 'space-a',
    });
    expect(created).not.toHaveProperty('effective_track_id');
    expect(statusQuery?.get('profile_id')).toBe('audio-track-b');
    expect(statusQuery?.get('task_id')).toBe('81');
    expect(status).toMatchObject({ effective_track_id: 'audio-b', task: { status: 'succeeded' } });
    expect(status.url).toMatch(/^https?:\/\/.+\/profiles\/audio-track-b\/tasks\/81\/master\.m3u8$/);
  });

  it('signal 取消时拒绝创建请求', async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(createAudioReload(9, 'audio-b', controller.signal)).rejects.toBeDefined();
  });
});

describe('ABR 显式生成 API', () => {
  it('仅在调用时 POST hls-abr 并返回任务信息', async () => {
    let body: unknown;
    server.use(
      http.post('*/api/play/9/hls-abr', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(
          {
            task_id: 77,
            profile_id: 'abr-h264',
            url: '/api/play/hls/9/profiles/abr-h264/master.m3u8',
          },
          { status: 202 },
        );
      }),
    );
    const result = await createHLSABR(9, { priority: 8, force_rebuild: true });
    expect(body).toEqual({ priority: 8, force_rebuild: true });
    expect(result.task_id).toBe(77);
    expect(result.profile_id).toBe('abr-h264');
    expect(result.url).toMatch(/\/api\/play\/hls\/9\/profiles\/abr-h264\/master\.m3u8$/);
  });
});

describe('negotiate（FR-53 编码协商 API）', () => {
  it('上报客户端能力并把相对 URL 绝对化', async () => {
    const captured: { caps?: Record<string, boolean> } = {};
    server.use(
      http.post('*/api/play/9/negotiate', async ({ request }) => {
        const reqBody = (await request.json()) as { client_caps?: Record<string, boolean> };
        captured.caps = reqBody.client_caps;
        return HttpResponse.json({
          codec: 'av1',
          path: 'fmp4',
          url: '/api/play/hls/9/index.m3u8',
          mime: 'video/mp4; codecs="av01.0.05M.08"',
          fallback_url: '/api/play/9/stream',
          frame_presentation: {
            marker: { bits: 9, cell_size: 8, threshold: 160, x: 16, y: 16 },
            nominal_frame_rate: 30,
            timeline: [
              { media_time: 0, source_frame_index: 0, stable_frame_id: 'binary-marker:0' },
              { media_time: 1 / 30, source_frame_index: 1, stable_frame_id: 'binary-marker:1' },
            ],
          },
        });
      }),
    );

    const desc = await negotiate(9, { h265: false, av1: true, vp9: false });

    expect(captured.caps).toEqual({ h265: false, av1: true, vp9: false });
    expect(desc.codec).toBe('av1');
    expect(desc.path).toBe('fmp4');
    expect(desc.url).toMatch(/^https?:\/\/.+\/api\/play\/hls\/9\/index\.m3u8$/);
    expect(desc.fallbackUrl).toMatch(/^https?:\/\/.+\/api\/play\/9\/stream$/);
    expect(desc.framePresentation).toEqual({
      marker: { bits: 9, cellSize: 8, threshold: 160, x: 16, y: 16 },
      nominalFrameRate: 30,
      timeline: [
        { mediaTime: 0, sourceFrameIndex: 0, stableFrameId: 'binary-marker:0' },
        { mediaTime: 1 / 30, sourceFrameIndex: 1, stableFrameId: 'binary-marker:1' },
      ],
    });
  });

  it('h264/TS 描述符无 fallbackUrl', async () => {
    server.use(
      http.post('*/api/play/2/negotiate', () =>
        HttpResponse.json({ codec: 'h264', path: 'ts', url: '/api/play/hls/2/master' }),
      ),
    );

    const desc = await negotiate(2, { h265: false, av1: false, vp9: false });

    expect(desc.codec).toBe('h264');
    expect(desc.path).toBe('ts');
    expect(desc.fallbackUrl).toBeUndefined();
  });
});
