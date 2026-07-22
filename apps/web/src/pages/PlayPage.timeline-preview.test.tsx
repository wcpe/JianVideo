import { render, screen, waitFor, act } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import type { PreparedPreviewTrack } from '@jianvideo/player-core';
import type { TimelinePreviewStatus } from '@/api/play';
import { server } from '@/mocks/beforeAll';
import PlayPage from './PlayPage';

const { getTimelinePreviewStatus, getTask, parseTimelinePreviewVtt, playerState } = vi.hoisted(
  () => ({
    getTimelinePreviewStatus: vi.fn(),
    getTask: vi.fn(),
    parseTimelinePreviewVtt: vi.fn(),
    playerState: { props: {} as Record<string, unknown> },
  }),
);

vi.mock('@/api/play', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/play')>()),
  getTimelinePreviewStatus,
}));
vi.mock('@/api/tasks', () => ({ getTask }));
vi.mock('@/utils/timeline-preview', () => ({ parseTimelinePreviewVtt }));
vi.mock('@/components/VideoPlayer', () => ({
  default: (props: Record<string, unknown>) => {
    playerState.props = props;
    return <div data-testid="video-player" />;
  },
}));

const TRACK: PreparedPreviewTrack = {
  cues: [
    {
      startTime: 0,
      endTime: 5,
      sprite: { assetId: 'sprite-url', x: 0, y: 0, width: 160, height: 90 },
    },
  ],
  generationId: 'generation-a',
  mediaId: '1',
  profileId: 'timeline-v1',
  sourceFingerprint: 'source-a',
};

function media(id: number) {
  return {
    id,
    library_id: 1,
    file_path: `D:/video/${id}.mp4`,
    file_name: `${id}.mp4`,
    file_size: 100,
    format: 'mp4',
    video_codec: 'h264',
    audio_codec: 'aac',
    duration: 10,
    width: 640,
    height: 360,
    bitrate: 1000,
    subtitle_tracks: '',
    added_at: '',
    modified_at: '',
  };
}

const TIMELINE_TASK_POLL_INTERVAL = 1000;

async function startPendingPolling() {
  const pending = deferredValue<TimelinePreviewStatus>();
  getTimelinePreviewStatus.mockReturnValueOnce(pending.promise);
  const page = renderPage();
  await screen.findByTestId('video-player');
  vi.useFakeTimers();
  pending.resolve({
    duration: 10,
    generation_id: 'generation-pending',
    profile_id: 'timeline-v1',
    status: 'pending',
    task_id: 42,
    version: 1,
  });
  await act(async () => Promise.resolve());
  return page;
}

function deferredValue<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function deferredResponse() {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function renderPage(route = '/play/1') {
  const router = createMemoryRouter(
    [
      { path: '/play/:id', element: <PlayPage /> },
      { path: '/other', element: <div>其它页面</div> },
    ],
    { initialEntries: [route] },
  );
  const result = render(
    <MantineProvider>
      <RouterProvider router={router} />
    </MantineProvider>,
  );
  return { ...result, router };
}

function availableStatus(mediaID = 1, generation = 'generation-a') {
  return {
    duration: 10,
    generation_id: generation,
    profile_id: 'timeline-v1',
    source_fingerprint: `source-${mediaID}`,
    sprite_urls: { 'sprite-001.jpg': `https://example.test/${mediaID}/sprite.jpg` },
    status: 'available' as const,
    version: 1,
    vtt_url: `https://example.test/${mediaID}/${generation}.vtt`,
  };
}

beforeEach(() => {
  playerState.props = {};
  getTimelinePreviewStatus.mockReset();
  getTask.mockReset();
  parseTimelinePreviewVtt.mockReset();
  server.use(
    http.get('*/api/library/media/:id', ({ params }) =>
      HttpResponse.json(media(Number(params.id))),
    ),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('PlayPage 时间轴预览', () => {
  it('available 时只加载一次 VTT 并把轨道和 sprite URL 传给播放器', async () => {
    const status = availableStatus();
    getTimelinePreviewStatus.mockResolvedValue(status);
    parseTimelinePreviewVtt.mockReturnValue(TRACK);
    const fetchVtt = vi.fn().mockResolvedValue(new Response('WEBVTT'));
    vi.stubGlobal('fetch', fetchVtt);

    renderPage();

    await screen.findByTestId('video-player');
    await waitFor(() => expect(playerState.props.previewTrack).toBe(TRACK));
    expect(getTimelinePreviewStatus).toHaveBeenCalledTimes(1);
    expect(fetchVtt).toHaveBeenCalledTimes(1);
    expect(parseTimelinePreviewVtt).toHaveBeenCalledWith('WEBVTT', {
      generationId: status.generation_id,
      mediaId: '1',
      profileId: status.profile_id,
      sourceFingerprint: status.source_fingerprint,
      spriteUrls: status.sprite_urls,
    });
    expect(playerState.props.previewSpriteUrls).toEqual(status.sprite_urls);
  });

  it('pending/running 超过三次后成功仍会复查状态并加载 VTT', async () => {
    getTask
      .mockResolvedValueOnce({ id: '42', status: 'pending' })
      .mockResolvedValueOnce({ id: '42', status: 'running' })
      .mockResolvedValueOnce({ id: '42', status: 'running' })
      .mockResolvedValueOnce({ id: '42', status: 'succeeded' });
    parseTimelinePreviewVtt.mockReturnValue(TRACK);
    const fetchVtt = vi.fn().mockResolvedValue(new Response('WEBVTT'));
    vi.stubGlobal('fetch', fetchVtt);

    await startPendingPolling();
    getTimelinePreviewStatus.mockResolvedValueOnce(availableStatus());
    expect(playerState.props.previewTrack).toBeUndefined();

    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL * 4));

    expect(getTask).toHaveBeenCalledTimes(4);
    expect(getTask.mock.calls.every(([, signal]) => signal instanceof AbortSignal)).toBe(true);
    expect(getTimelinePreviewStatus).toHaveBeenCalledTimes(2);
    expect(fetchVtt).toHaveBeenCalledTimes(1);
    expect(playerState.props.previewTrack).toBe(TRACK);
  });

  it.each(['failed', 'canceled'] as const)('任务 %s 后停止轮询且不影响播放器', async (status) => {
    getTask.mockResolvedValueOnce({ id: '42', status });

    await startPendingPolling();
    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL * 10));

    expect(getTask).toHaveBeenCalledTimes(1);
    expect(getTimelinePreviewStatus).toHaveBeenCalledTimes(1);
    expect(playerState.props.previewTrack).toBeUndefined();
    expect(screen.getByTestId('video-player')).toBeInTheDocument();
  });

  it('任务查询临时失败后有限退避并继续轮询', async () => {
    getTask
      .mockRejectedValueOnce(new Error('临时网络失败'))
      .mockResolvedValueOnce({ id: '42', status: 'running' })
      .mockResolvedValueOnce({ id: '42', status: 'succeeded' });
    parseTimelinePreviewVtt.mockReturnValue(TRACK);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('WEBVTT')));

    await startPendingPolling();
    getTimelinePreviewStatus.mockResolvedValueOnce(availableStatus());

    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL));
    expect(getTask).toHaveBeenCalledTimes(1);
    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL * 2 - 1));
    expect(getTask).toHaveBeenCalledTimes(1);
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(getTask).toHaveBeenCalledTimes(2);
    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL));
    expect(getTask).toHaveBeenCalledTimes(3);
    expect(playerState.props.previewTrack).toBe(TRACK);
  });

  it('卸载组件会立即取消 pending 轮询且不再请求任务', async () => {
    getTask.mockResolvedValue({ id: '42', status: 'running' });
    const { unmount } = await startPendingPolling();

    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL));
    expect(getTask).toHaveBeenCalledTimes(1);
    const signal = getTask.mock.calls[0]?.[1] as AbortSignal;

    unmount();

    expect(signal.aborted).toBe(true);
    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL * 5));
    expect(getTask).toHaveBeenCalledTimes(1);
  });

  it('切换媒体会立即取消 pending 轮询且不再请求旧任务', async () => {
    getTask.mockResolvedValue({ id: '42', status: 'running' });
    const { router } = await startPendingPolling();

    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL));
    expect(getTask).toHaveBeenCalledTimes(1);
    const lastSignal = getTask.mock.calls[0]?.[1] as AbortSignal;

    getTimelinePreviewStatus.mockResolvedValueOnce(availableStatus(2, 'generation-new'));
    parseTimelinePreviewVtt.mockReturnValue({ ...TRACK, mediaId: '2' });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('NEW')));
    await act(async () => router.navigate('/play/2'));

    expect(lastSignal.aborted).toBe(true);
    await act(async () => vi.advanceTimersByTimeAsync(TIMELINE_TASK_POLL_INTERVAL * 5));
    expect(getTask).toHaveBeenCalledTimes(1);
    expect(playerState.props.previewTrack).toMatchObject({ mediaId: '2' });
  });

  it('切换媒体时中止旧请求并丢弃迟到的 generation', async () => {
    const oldVtt = deferredResponse();
    const fetchVtt = vi.fn((url: string, _init?: RequestInit) =>
      url.includes('/1/') ? oldVtt.promise : Promise.resolve(new Response('NEW')),
    );
    vi.stubGlobal('fetch', fetchVtt);
    getTimelinePreviewStatus.mockImplementation((mediaID: number) =>
      Promise.resolve(
        availableStatus(mediaID, mediaID === 1 ? 'generation-old' : 'generation-new'),
      ),
    );
    parseTimelinePreviewVtt.mockImplementation((_vtt: string, identity: { mediaId: string }) => ({
      ...TRACK,
      mediaId: identity.mediaId,
      generationId: identity.mediaId === '1' ? 'generation-old' : 'generation-new',
    }));
    const { router } = renderPage();
    await screen.findByTestId('video-player');
    await waitFor(() => expect(fetchVtt).toHaveBeenCalledTimes(1));

    await act(async () => router.navigate('/play/2'));
    await waitFor(() => expect(playerState.props.previewTrack).toMatchObject({ mediaId: '2' }));
    const oldSignal = (fetchVtt.mock.calls[0]?.[1] as RequestInit | undefined)?.signal;
    expect(oldSignal?.aborted).toBe(true);

    oldVtt.resolve(new Response('OLD'));
    await act(async () => Promise.resolve());
    expect(playerState.props.previewTrack).toMatchObject({
      mediaId: '2',
      generationId: 'generation-new',
    });
  });
});
