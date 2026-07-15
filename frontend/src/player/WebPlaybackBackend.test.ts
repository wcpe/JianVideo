import { PlaybackCore } from '@jianvideo/player-core';
import type {
  PlaybackBackendEvent,
  PlaybackCommandContext,
  PlaybackSource,
  SeekRequest,
} from '@jianvideo/player-core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TrackResponse } from '@/api/subtitle';
import { WebPlaybackBackend, type WebPlaybackSourcePayload } from './WebPlaybackBackend';

interface FakeMpegtsPlayer {
  attachMediaElement: ReturnType<typeof vi.fn>;
  destroy: ReturnType<typeof vi.fn>;
  handlers: Map<string, (...args: unknown[]) => void>;
  load: ReturnType<typeof vi.fn>;
  on: ReturnType<typeof vi.fn>;
  pause: ReturnType<typeof vi.fn>;
  play: ReturnType<typeof vi.fn>;
  unload: ReturnType<typeof vi.fn>;
}

interface FakeHlsInstance {
  attachMedia: ReturnType<typeof vi.fn>;
  autoLevelCapping: number;
  currentLevel: number;
  destroy: ReturnType<typeof vi.fn>;
  handlers: Map<string, (...args: unknown[]) => void>;
  levels: Array<{ bitrate: number; height: number; width: number }>;
  loadSource: ReturnType<typeof vi.fn>;
  loadingEnabled: boolean;
  startLoad: ReturnType<typeof vi.fn>;
  stopLoad: ReturnType<typeof vi.fn>;
}

const mpegtsMock = vi.hoisted(() => ({
  createPlayer: vi.fn(),
}));

const hlsMock = vi.hoisted(() => ({
  configs: [] as Array<{
    autoStartLoad?: boolean;
    startFragPrefetch?: boolean;
    xhrSetup?: (xhr: XMLHttpRequest) => void;
  }>,
  instances: [] as FakeHlsInstance[],
  supported: true,
}));

const playApiMock = vi.hoisted(() => ({
  createAudioReload: vi.fn(),
  getHLSStatus: vi.fn(),
}));

vi.mock('mpegts.js', () => ({
  default: {
    createPlayer: (...args: unknown[]) => mpegtsMock.createPlayer(...args),
  },
}));

vi.mock('@/api/play', () => ({
  createAudioReload: (...args: unknown[]) => playApiMock.createAudioReload(...args),
  getHLSStatus: (...args: unknown[]) => playApiMock.getHLSStatus(...args),
}));

vi.mock('hls.js', () => {
  class FakeHls {
    static Events = { ERROR: 'error', LEVEL_SWITCHED: 'level', MANIFEST_PARSED: 'manifest' };
    static isSupported() {
      return hlsMock.supported;
    }

    attachMedia = vi.fn((media: HTMLMediaElement) => {
      media.playbackRate = media.defaultPlaybackRate;
      media.dispatchEvent(new Event('ratechange'));
    });
    autoLevelCapping = -1;
    currentLevel = -1;
    destroy = vi.fn();
    handlers = new Map<string, (...args: unknown[]) => void>();
    levels = [
      { bitrate: 2_500_000, height: 720, width: 1280 },
      { bitrate: 1_000_000, height: 480, width: 854 },
    ];
    loadSource = vi.fn();
    loadingEnabled = false;
    startLoad = vi.fn(() => {
      this.loadingEnabled = true;
    });
    stopLoad = vi.fn(() => {
      this.loadingEnabled = false;
    });

    constructor(config: {
      autoStartLoad?: boolean;
      startFragPrefetch?: boolean;
      xhrSetup?: (xhr: XMLHttpRequest) => void;
    } = {}) {
      hlsMock.configs.push(config);
      hlsMock.instances.push(this);
    }

    on(event: string, handler: (...args: unknown[]) => void) {
      this.handlers.set(event, handler);
    }
  }

  return { default: FakeHls };
});

function createMpegtsPlayer(): FakeMpegtsPlayer {
  const handlers = new Map<string, (...args: unknown[]) => void>();
  return {
    attachMediaElement: vi.fn(),
    destroy: vi.fn(),
    handlers,
    load: vi.fn(),
    on: vi.fn((event: string, handler: (...args: unknown[]) => void) =>
      handlers.set(event, handler),
    ),
    pause: vi.fn(),
    play: vi.fn(() => Promise.resolve()),
    unload: vi.fn(),
  };
}

function createVideo() {
  const video = document.createElement('video');
  Object.defineProperty(video, 'play', {
    configurable: true,
    value: vi.fn(() => Promise.resolve()),
  });
  Object.defineProperty(video, 'pause', { configurable: true, value: vi.fn() });
  Object.defineProperty(video, 'load', { configurable: true, value: vi.fn() });
  return video;
}

function stubVideoFrameCallbacks(video: HTMLVideoElement) {
  let nextId = 1;
  const callbacks = new Map<number, VideoFrameRequestCallback>();
  Object.assign(video, {
    cancelVideoFrameCallback: vi.fn((id: number) => callbacks.delete(id)),
    requestVideoFrameCallback: vi.fn((callback: VideoFrameRequestCallback) => {
      const id = nextId++;
      callbacks.set(id, callback);
      return id;
    }),
  });
  return {
    present(mediaTime: number) {
      const id = Math.min(...callbacks.keys());
      const callback = callbacks.get(id);
      callbacks.delete(id);
      callback?.(performance.now(), { mediaTime } as VideoFrameCallbackMetadata);
    },
  };
}

function createSource(
  id: string,
  kind: WebPlaybackSourcePayload['kind'],
  url = `/${id}`,
  extras: Partial<WebPlaybackSourcePayload> = {},
): PlaybackSource {
  return {
    id,
    mode: kind === 'native' ? 'direct' : kind === 'hls' ? 'adaptive' : 'stream',
    payload: { kind, url, ...extras } satisfies WebPlaybackSourcePayload,
  };
}

function createAudioTrackResponse(): TrackResponse {
  return {
    tracks: ['audio-a', 'audio-b', 'audio-c'].map((id) => ({
      available: true,
      capability: 'reload' as const,
      id,
      kind: 'audio' as const,
      label: id,
      source: 'embedded' as const,
    })),
    selection: {
      audio: { effectiveTrackId: 'audio-a', selectedTrackId: 'audio-a' },
      subtitle: { effectiveTrackId: null, selectedTrackId: null },
    },
    backend: {},
    sources: {},
  };
}

function createAudioReload(trackId: string) {
  return {
    created: {
      profile_id: `profile-${trackId}`,
      requested_track_id: trackId,
      space_id: 'space-a',
      task_id: `task-${trackId}`,
      url: `/${trackId}.m3u8`,
    },
    status: {
      available: true,
      effective_track_id: trackId,
      profile_id: `profile-${trackId}`,
      task: { id: `task-${trackId}`, progress: 100, status: 'succeeded' as const },
      url: `/${trackId}.m3u8`,
    },
  };
}

function createCommand(
  sourceId: string,
  sourceEpoch: number,
  requestId = sourceEpoch,
): PlaybackCommandContext {
  return { requestId, sourceEpoch, sourceId };
}

function createSeek(command: PlaybackCommandContext, targetTime: number): SeekRequest {
  return {
    ...command,
    boundaryPolicy: 'clamp',
    reason: 'user',
    requestedTime: targetTime,
    targetTime,
  };
}

function timeRanges(ranges: Array<[number, number]>): TimeRanges {
  return {
    length: ranges.length,
    start: (index: number) => ranges[index][0],
    end: (index: number) => ranges[index][1],
  };
}

function stubTimeline(video: HTMLVideoElement, duration = 120) {
  let currentTime = 0;
  Object.defineProperties(video, {
    currentTime: {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    },
    duration: { configurable: true, get: () => duration },
    seekable: { configurable: true, get: () => timeRanges([[0, duration]]) },
  });
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

async function flushMicrotasks() {
  await Promise.resolve();
  await Promise.resolve();
}

async function flushTasks() {
  await new Promise<void>((resolve) => setTimeout(resolve, 0));
}

async function waitForHlsInstance() {
  await vi.dynamicImportSettled();
  await flushMicrotasks();
  return hlsMock.instances.at(-1)!;
}

function completeHls(video: HTMLVideoElement, hls: FakeHlsInstance): void {
  hls.handlers.get('manifest')?.();
  video.dispatchEvent(new Event('canplay'));
}

beforeEach(() => {
  vi.stubGlobal('MediaSource', { isTypeSupported: () => true });
  hlsMock.configs.length = 0;
  hlsMock.instances.length = 0;
  hlsMock.supported = true;
  playApiMock.createAudioReload.mockReset();
  playApiMock.getHLSStatus.mockReset();
  mpegtsMock.createPlayer.mockReset();
  mpegtsMock.createPlayer.mockImplementation(() => createMpegtsPlayer());
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('WebPlaybackBackend ready 时序', () => {
  it('native 立即完成，mpegts 等 loadeddata，HLS 等清单与 canplay', async () => {
    const nativeBackend = new WebPlaybackBackend(createVideo());
    await expect(
      nativeBackend.load(createSource('native', 'native'), createCommand('native', 1)),
    ).resolves.toBeUndefined();

    const mpegPlayer = createMpegtsPlayer();
    mpegtsMock.createPlayer.mockReturnValueOnce(mpegPlayer);
    const mpegBackend = new WebPlaybackBackend(createVideo());
    let mpegSettled = false;
    const mpegLoad = mpegBackend
      .load(createSource('ts', 'mpegts'), createCommand('ts', 1))
      .then(() => {
        mpegSettled = true;
      });
    await flushMicrotasks();
    expect(mpegSettled).toBe(false);
    mpegPlayer.handlers.get('loadeddata')?.();
    await mpegLoad;
    expect(mpegSettled).toBe(true);

    const hlsVideo = createVideo();
    const hlsBackend = new WebPlaybackBackend(hlsVideo);
    let hlsSettled = false;
    const hlsLoad = hlsBackend
      .load(createSource('hls', 'hls'), createCommand('hls', 1))
      .then(() => {
        hlsSettled = true;
      });
    const hls = await waitForHlsInstance();
    expect(hlsSettled).toBe(false);
    completeHls(hlsVideo, hls);
    await hlsLoad;
    expect(hlsSettled).toBe(true);
  });

  it('HLS 候选不复用旧媒体 readyState，清单后仍等待本次 canplay', async () => {
    const video = createVideo();
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      value: video.HAVE_FUTURE_DATA,
    });
    const backend = new WebPlaybackBackend(video);
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    vi.mocked(video.load).mockClear();

    let settled = false;
    const transaction = backend
      .transactAudioSource(
        '/audio-b.m3u8',
        'space-a',
        createCommand('source', 1),
        new AbortController().signal,
      )
      .then(() => {
        settled = true;
      });
    const hls = await waitForHlsInstance();
    hls.handlers.get('manifest')?.();
    await flushTasks();

    expect(settled).toBe(false);
    expect(video.load).toHaveBeenCalledOnce();

    video.dispatchEvent(new Event('canplay'));
    await transaction;
    expect(settled).toBe(true);
  });

  it('HLS 候选忽略清单前迟到的旧 canplay', async () => {
    const video = createVideo();
    const backend = new WebPlaybackBackend(video);
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));

    let settled = false;
    const transaction = backend
      .transactAudioSource(
        '/audio-b.m3u8',
        'space-a',
        createCommand('source', 1),
        new AbortController().signal,
      )
      .then(() => {
        settled = true;
      });
    const hls = await waitForHlsInstance();
    video.dispatchEvent(new Event('canplay'));
    hls.handlers.get('manifest')?.();
    await flushTasks();

    expect(settled).toBe(false);

    video.dispatchEvent(new Event('canplay'));
    await transaction;
    expect(settled).toBe(true);
  });

  it('主 HLS 仅加载清单，播放后才启动分片，并暴露清晰度与加载分面', async () => {
    const video = createVideo();
    const backend = new WebPlaybackBackend(video);
    const loading = backend.load(createSource('quality-hls', 'hls'), createCommand('quality-hls', 1));
    const hls = await waitForHlsInstance();

    expect(hlsMock.configs.at(-1)).toMatchObject({ autoStartLoad: false, startFragPrefetch: false });
    expect(video.preload).toBe('none');
    expect(hls.startLoad).not.toHaveBeenCalled();

    hls.handlers.get('manifest')?.();
    await loading;
    expect(backend.getSnapshot().capabilities).toMatchObject({
      loadControl: 'available',
      quality: 'available',
    });
    expect(backend.quality.getState().qualities).toHaveLength(2);

    await backend.play(createCommand('quality-hls', 1, 2));
    expect(hls.startLoad).toHaveBeenCalledOnce();
    await backend.loadControl.stopLoading(createCommand('quality-hls', 1, 3));
    expect(hls.stopLoad).toHaveBeenCalledOnce();
  });

  it('HLS 不支持、导入失败、fatal 与超时均严格拒绝且不创建 mpegts', async () => {
    hlsMock.supported = false;
    const unsupported = new WebPlaybackBackend(createVideo());
    await expect(
      unsupported.load(createSource('unsupported-hls', 'hls'), createCommand('unsupported-hls', 1)),
    ).rejects.toThrow('不支持 hls.js');

    const importFailure = new WebPlaybackBackend(createVideo(), {
      loadHlsModule: () => Promise.reject(new Error('模块缺失')),
    });
    await expect(
      importFailure.load(createSource('import-failure', 'hls'), createCommand('import-failure', 1)),
    ).rejects.toThrow('模块缺失');

    hlsMock.supported = true;
    const fatalBackend = new WebPlaybackBackend(createVideo());
    const fatalLoad = fatalBackend.load(createSource('fatal', 'hls'), createCommand('fatal', 1));
    const fatalHls = await waitForHlsInstance();
    fatalHls.handlers.get('error')?.('error', { fatal: true });
    await expect(fatalLoad).rejects.toThrow('致命错误');

    vi.useFakeTimers();
    const timeoutBackend = new WebPlaybackBackend(createVideo(), { hlsReadyTimeoutMs: 5 });
    const timeoutLoad = timeoutBackend.load(
      createSource('timeout', 'hls'),
      createCommand('timeout', 1),
    );
    const timeoutRejected = expect(timeoutLoad).rejects.toThrow('就绪超时');
    await waitForHlsInstance();
    await vi.advanceTimersByTimeAsync(5);
    await timeoutRejected;

    expect(mpegtsMock.createPlayer).not.toHaveBeenCalled();
  });
});

describe('WebPlaybackBackend 音轨源事务', () => {
  it('成功切换后恢复时间、倍速和播放意图，并传播 Space/Auth 请求头', async () => {
    const video = createVideo();
    stubTimeline(video);
    const backend = new WebPlaybackBackend(video, {
      getHlsRequestHeaders: () => ({ Authorization: 'Bearer token-a' }),
    });
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    video.currentTime = 37;
    video.playbackRate = 1.5;
    await backend.play(createCommand('source', 1, 2));

    const transaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1, 2),
      new AbortController().signal,
    );
    const hls = await waitForHlsInstance();
    const headers: Record<string, string> = {};
    const xhr = {
      withCredentials: false,
      setRequestHeader: (name: string, value: string) => {
        headers[name] = value;
      },
    } as XMLHttpRequest;
    hlsMock.configs.at(-1)?.xhrSetup?.(xhr);
    completeHls(video, hls);
    await transaction;

    expect(xhr.withCredentials).toBe(true);
    expect(headers).toEqual({
      Authorization: 'Bearer token-a',
      'X-JianVideo-Space-Id': 'space-a',
    });
    expect(video.currentTime).toBe(37);
    expect(video.playbackRate).toBe(1.5);
    expect(video.play).toHaveBeenCalledTimes(2);
    expect(backend.getSnapshot()).toMatchObject({ state: 'playing' });
    expect(mpegtsMock.createPlayer).not.toHaveBeenCalled();
  });

  it('候选期间后发 play、seek 与 ratechange 不被旧恢复点覆盖', async () => {
    const video = createVideo();
    stubTimeline(video);
    const backend = new WebPlaybackBackend(video);
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    video.currentTime = 18;

    const transaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1),
      new AbortController().signal,
    );
    const hls = await waitForHlsInstance();
    await backend.play(createCommand('source', 1, 2));
    await backend.seek(createSeek(createCommand('source', 1, 3), 52));
    video.playbackRate = 1.75;
    video.dispatchEvent(new Event('ratechange'));
    completeHls(video, hls);

    await expect(transaction).resolves.toBeUndefined();
    expect(video.getAttribute('src')).not.toBe('/old.mp4');
    expect(video.currentTime).toBe(52);
    expect(video.playbackRate).toBe(1.75);
    expect(backend.getSnapshot()).toMatchObject({ requestId: 3, state: 'playing' });
  });

  it('异步恢复播放期间后发 pause 抢占旧播放完成且仍提交候选源', async () => {
    const video = createVideo();
    stubTimeline(video);
    const playGate = deferred<void>();
    const play = vi
      .fn<() => Promise<void>>()
      .mockResolvedValueOnce(undefined)
      .mockReturnValueOnce(playGate.promise)
      .mockResolvedValueOnce(undefined);
    Object.defineProperty(video, 'play', { configurable: true, value: play });
    const backend = new WebPlaybackBackend(video);
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    video.currentTime = 33;
    video.playbackRate = 1.25;
    await backend.play(createCommand('source', 1, 2));

    const transaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1, 2),
      new AbortController().signal,
    );
    const hls = await waitForHlsInstance();
    completeHls(video, hls);
    await vi.waitFor(() => expect(play).toHaveBeenCalledTimes(2));
    await backend.pause(createCommand('source', 1, 3));
    playGate.resolve();

    await expect(transaction).resolves.toBeUndefined();
    expect(video.getAttribute('src')).not.toBe('/old.mp4');
    expect(video.currentTime).toBe(33);
    expect(video.playbackRate).toBe(1.25);
    expect(backend.getSnapshot()).toMatchObject({ requestId: 3, state: 'paused' });
  });

  it('提交前 fatal 按最新控制态回滚原源且不发布终态错误', async () => {
    const video = createVideo();
    stubTimeline(video);
    const events: PlaybackBackendEvent[] = [];
    const onPlaybackError = vi.fn();
    const backend = new WebPlaybackBackend(video, { onPlaybackError });
    backend.subscribe((event) => events.push(event));
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    video.currentTime = 22;
    video.playbackRate = 0.75;

    const transaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1),
      new AbortController().signal,
    );
    const hls = await waitForHlsInstance();
    await backend.seek(createSeek(createCommand('source', 1, 2), 44));
    await backend.play(createCommand('source', 1, 3));
    video.playbackRate = 1.5;
    video.dispatchEvent(new Event('ratechange'));
    hls.handlers.get('manifest')?.();
    hls.handlers.get('error')?.('error', { fatal: true });

    await expect(transaction).rejects.toThrow('致命错误');
    expect(video.getAttribute('src')).toBe('/old.mp4');
    expect(video.currentTime).toBe(44);
    expect(video.playbackRate).toBe(1.5);
    expect(backend.getSnapshot()).toMatchObject({ error: null, requestId: 3, state: 'playing' });
    expect(onPlaybackError).not.toHaveBeenCalled();
    expect(events.filter((event) => event.type === 'error')).toHaveLength(0);
    expect(mpegtsMock.createPlayer).not.toHaveBeenCalled();
  });

  it('native 原源的候选 HLS 媒体错误由事务回滚且提交前不外泄', async () => {
    const video = createVideo();
    stubTimeline(video);
    const events: PlaybackBackendEvent[] = [];
    const onPlaybackError = vi.fn();
    const backend = new WebPlaybackBackend(video, { onPlaybackError });
    backend.subscribe((event) => events.push(event));
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    video.currentTime = 26;

    const transaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1),
      new AbortController().signal,
    );
    await waitForHlsInstance();
    Object.defineProperty(video, 'error', { configurable: true, get: () => ({ code: 3 }) });
    video.dispatchEvent(new Event('error'));

    await expect(transaction).rejects.toThrow('媒体解码失败');
    expect(video.getAttribute('src')).toBe('/old.mp4');
    expect(video.currentTime).toBe(26);
    expect(backend.getSnapshot()).toMatchObject({ error: null, state: 'paused' });
    expect(onPlaybackError).not.toHaveBeenCalled();
    expect(events.filter((event) => event.type === 'error')).toHaveLength(0);
  });

  it('提交后当前 HLS fatal 发布受控错误且不回滚或创建 mpegts', async () => {
    const video = createVideo();
    stubTimeline(video);
    const events: PlaybackBackendEvent[] = [];
    const onPlaybackError = vi.fn();
    const backend = new WebPlaybackBackend(video, { onPlaybackError });
    backend.subscribe((event) => events.push(event));
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));

    const transaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1),
      new AbortController().signal,
    );
    const hls = await waitForHlsInstance();
    completeHls(video, hls);
    await transaction;
    vi.mocked(video.load).mockClear();
    hls.handlers.get('error')?.('error', { fatal: true });
    hls.handlers.get('error')?.('error', { fatal: true });
    await flushMicrotasks();

    const expectedError = {
      category: 'media',
      code: 'HLS_FATAL',
      message: 'HLS 播放发生致命错误',
    };
    expect(onPlaybackError).toHaveBeenCalledOnce();
    expect(onPlaybackError).toHaveBeenCalledWith(expectedError);
    expect(events.filter((event) => event.type === 'error')).toHaveLength(1);
    expect(backend.getSnapshot()).toMatchObject({ error: expectedError, state: 'error' });
    expect(video.load).not.toHaveBeenCalled();
    expect(video.getAttribute('src')).not.toBe('/old.mp4');
    expect(hls.destroy).toHaveBeenCalledOnce();
    expect(mpegtsMock.createPlayer).not.toHaveBeenCalled();
  });

  it('manifest 后 canplay 前 fatal 会回滚暂停态原源', async () => {
    const video = createVideo();
    stubTimeline(video);
    const backend = new WebPlaybackBackend(video);
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    video.currentTime = 22;
    video.playbackRate = 0.75;

    const transaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1),
      new AbortController().signal,
    );
    const hls = await waitForHlsInstance();
    hls.handlers.get('manifest')?.();
    hls.handlers.get('error')?.('error', { fatal: true });

    await expect(transaction).rejects.toThrow('致命错误');
    expect(video.getAttribute('src')).toBe('/old.mp4');
    expect(video.currentTime).toBe(22);
    expect(video.playbackRate).toBe(0.75);
    expect(backend.getSnapshot().state).toBe('paused');
    expect(mpegtsMock.createPlayer).not.toHaveBeenCalled();
  });

  it('canplay 后恢复播放尚未完成时 fatal 仍会回滚原源', async () => {
    const video = createVideo();
    stubTimeline(video);
    const playGate = deferred<void>();
    const play = vi
      .fn<() => Promise<void>>()
      .mockResolvedValueOnce(undefined)
      .mockReturnValueOnce(playGate.promise)
      .mockResolvedValueOnce(undefined);
    Object.defineProperty(video, 'play', { configurable: true, value: play });
    const backend = new WebPlaybackBackend(video);
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    video.currentTime = 48;
    video.playbackRate = 1.75;
    await backend.play(createCommand('source', 1, 2));

    const transaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1, 2),
      new AbortController().signal,
    );
    const hls = await waitForHlsInstance();
    completeHls(video, hls);
    await flushMicrotasks();
    hls.handlers.get('error')?.('error', { fatal: true });

    await expect(transaction).rejects.toThrow('致命错误');
    playGate.resolve();
    expect(video.getAttribute('src')).toBe('/old.mp4');
    expect(video.currentTime).toBe(48);
    expect(video.playbackRate).toBe(1.75);
    expect(backend.getSnapshot().state).toBe('playing');
    expect(mpegtsMock.createPlayer).not.toHaveBeenCalled();
  });

  it('较新音轨请求在源事务前失败时，旧候选回滚且保留较新命令', async () => {
    const video = createVideo();
    stubTimeline(video);
    const reload = createAudioReload('audio-b');
    playApiMock.createAudioReload
      .mockResolvedValueOnce(reload.created)
      .mockRejectedValueOnce(new Error('音轨 C 创建失败'));
    playApiMock.getHLSStatus.mockResolvedValueOnce(reload.status);
    const backend = new WebPlaybackBackend(video);
    await backend.load(
      createSource('source', 'native', '/old.mp4', {
        mediaId: 9,
        trackResponse: createAudioTrackResponse(),
      }),
      createCommand('source', 1),
    );
    video.currentTime = 31;
    video.playbackRate = 1.25;
    await backend.play(createCommand('source', 1));

    const first = backend.tracks.selectTrack('audio', 'audio-b', createCommand('source', 1, 2));
    const firstRejected = expect(first).rejects.toMatchObject({ name: 'AbortError' });
    const oldHls = await waitForHlsInstance();
    const second = backend.tracks.selectTrack('audio', 'audio-c', createCommand('source', 1, 3));

    await expect(second).rejects.toThrow('音轨 C 创建失败');
    await firstRejected;
    expect(oldHls.destroy).toHaveBeenCalledOnce();
    expect(video.getAttribute('src')).toBe('/old.mp4');
    expect(video.currentTime).toBe(31);
    expect(video.playbackRate).toBe(1.25);
    expect(backend.getSnapshot()).toMatchObject({ requestId: 3, state: 'playing' });
  });

  it('较新源事务 generation 接管后，旧事务不得反向回滚', async () => {
    const video = createVideo();
    stubTimeline(video);
    const backend = new WebPlaybackBackend(video);
    await backend.load(createSource('source', 'native', '/old.mp4'), createCommand('source', 1));
    await backend.play(createCommand('source', 1));
    const oldController = new AbortController();
    const oldTransaction = backend.transactAudioSource(
      '/audio-b.m3u8',
      'space-a',
      createCommand('source', 1),
      oldController.signal,
    );
    const oldRejected = expect(oldTransaction).rejects.toMatchObject({ name: 'AbortError' });
    const oldHls = await waitForHlsInstance();
    await backend.play(createCommand('source', 1, 2));

    const currentTransaction = backend.transactAudioSource(
      '/audio-c.m3u8',
      'space-a',
      createCommand('source', 1, 2),
      new AbortController().signal,
    );
    const currentHls = await waitForHlsInstance();
    oldController.abort();
    completeHls(video, currentHls);

    await oldRejected;
    await currentTransaction;
    expect(oldHls.destroy).toHaveBeenCalledOnce();
    expect(currentHls.destroy).not.toHaveBeenCalled();
    expect(video.getAttribute('src')).not.toBe('/old.mp4');
    expect(backend.getSnapshot()).toMatchObject({ requestId: 2, state: 'playing' });
  });
});

describe('WebPlaybackBackend 帧呈现能力', () => {
  it('普通播放源静态声明 seek 可用，真实赋值失败返回受控 media 错误', async () => {
    const video = createVideo();
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      get: () => 0,
      set: () => {
        throw new DOMException('不可定位');
      },
    });
    const backend = new WebPlaybackBackend(video);
    const command = createCommand('blocked', 1);

    await backend.load(createSource('blocked', 'native'), command);
    const result = await backend.seek(createSeek(command, 10));

    expect(backend.getSnapshot().capabilities).toMatchObject({
      framePresentation: 'approximate',
      seek: 'available',
    });
    expect(result).toMatchObject({
      error: { category: 'media' },
      status: 'failed',
    });
  });

  it('按当前 facet 动态发布 exact、approximate 与 unavailable', async () => {
    const video = createVideo();
    const frames = stubVideoFrameCallbacks(video);
    const backend = new WebPlaybackBackend(video);
    const events: PlaybackBackendEvent[] = [];
    backend.subscribe((event) => events.push(event));
    const timeline = [
      { mediaTime: 0, stableFrameId: 'a' },
      { mediaTime: 0.04, stableFrameId: 'b' },
    ];
    const timelineOnlySource: PlaybackSource = {
      id: 'timeline-only',
      mode: 'direct',
      payload: {
        frameTimeline: timeline,
        kind: 'native',
        url: '/timeline-only.mp4',
      } satisfies WebPlaybackSourcePayload,
    };
    const exactSource: PlaybackSource = {
      id: 'exact',
      mode: 'direct',
      payload: {
        frameTimeline: timeline,
        kind: 'native',
        resolvePresentedFrameIdentity: () => ({ stableFrameId: 'a' }),
        url: '/exact.mp4',
      } satisfies WebPlaybackSourcePayload,
    };

    await backend.load(timelineOnlySource, createCommand('timeline-only', 1));
    expect(backend.getSnapshot().capabilities.framePresentation).toBe('approximate');

    await backend.load(exactSource, createCommand('exact', 2, 20));
    expect(backend.getSnapshot().capabilities.framePresentation).toBe('approximate');
    frames.present(0);
    expect(backend.getSnapshot().capabilities.framePresentation).toBe('exact');
    expect(events.at(-1)).toMatchObject({
      capabilities: { framePresentation: 'exact' },
      requestId: 20,
      sourceEpoch: 2,
      sourceId: 'exact',
      type: 'capabilitiesChanged',
    });

    await backend.load(createSource('approximate', 'native'), createCommand('approximate', 3));
    expect(backend.getSnapshot().capabilities.framePresentation).toBe('approximate');

    await backend.load(createSource('unsupported', 'unsupported'), createCommand('unsupported', 4));
    expect(backend.getSnapshot().capabilities.framePresentation).toBe('unavailable');
  });
});

describe('WebPlaybackBackend 命令与快照', () => {
  it('转发播放、暂停、Seek，并标准化时间区间', async () => {
    const video = createVideo();
    let currentTime = 15;
    Object.defineProperties(video, {
      buffered: {
        configurable: true,
        get: () =>
          timeRanges([
            [20, 40],
            [0, 10],
            [8, 25],
          ]),
      },
      currentTime: {
        configurable: true,
        get: () => currentTime,
        set: (value: number) => {
          currentTime = value;
        },
      },
      duration: { configurable: true, get: () => 120 },
      seekable: {
        configurable: true,
        get: () =>
          timeRanges([
            [0, 60],
            [55, 120],
          ]),
      },
    });
    const backend = new WebPlaybackBackend(video);
    const loadCommand = createCommand('native', 1, 1);

    await backend.load(createSource('native', 'native'), loadCommand);
    await backend.play(createCommand('native', 1, 2));
    await backend.pause(createCommand('native', 1, 3));
    const result = await backend.seek(createSeek(createCommand('native', 1, 4), 50));
    video.dispatchEvent(new Event('progress'));

    expect(video.play).toHaveBeenCalledOnce();
    expect(video.pause).toHaveBeenCalledOnce();
    expect(result).toMatchObject({ confirmedTime: 50, status: 'completed', targetTime: 50 });
    expect(backend.getSnapshot()).toMatchObject({
      buffered: [{ end: 40, start: 0 }],
      currentTime: 50,
      duration: 120,
      requestId: 4,
      seekable: [{ end: 120, start: 0 }],
    });
  });

  it('原生媒体 seeked 后从 seeking 回落到实际暂停态', async () => {
    const video = createVideo();
    stubTimeline(video);
    const backend = new WebPlaybackBackend(video);

    await backend.load(createSource('native', 'native'), createCommand('native', 1));
    video.dispatchEvent(new Event('seeking'));
    expect(backend.getSnapshot().state).toBe('seeking');

    video.dispatchEvent(new Event('seeked'));
    expect(backend.getSnapshot().state).toBe('paused');
  });

  it('mpegts.js 播放与暂停命令转发给现有内核', async () => {
    const video = createVideo();
    const player = createMpegtsPlayer();
    mpegtsMock.createPlayer.mockReturnValue(player);
    const backend = new WebPlaybackBackend(video);
    const loading = backend.load(createSource('ts', 'mpegts'), createCommand('ts', 1));
    player.handlers.get('loadeddata')?.();
    await loading;

    await backend.play(createCommand('ts', 1, 2));
    await backend.pause(createCommand('ts', 1, 3));

    expect(player.play).toHaveBeenCalledOnce();
    expect(player.pause).toHaveBeenCalledOnce();
  });

  it('拒绝同源旧 requestId 命令，媒体自发事件沿用最新命令代次', async () => {
    const video = createVideo();
    stubTimeline(video);
    const backend = new WebPlaybackBackend(video);
    const events: PlaybackBackendEvent[] = [];
    backend.subscribe((event) => events.push(event));

    await backend.load(createSource('native', 'native'), createCommand('native', 1, 10));
    await backend.play(createCommand('native', 1, 11));
    await backend.pause(createCommand('native', 1, 12));
    await backend.seek(createSeek(createCommand('native', 1, 13), 30));
    video.dispatchEvent(new Event('timeupdate'));
    await backend.play(createCommand('native', 1, 11));

    expect(video.play).toHaveBeenCalledOnce();
    expect(backend.getSnapshot().requestId).toBe(13);
    const lastEvent = events.at(-1);
    expect(lastEvent).toMatchObject({ requestId: 13, type: 'snapshotChanged' });
    expect(lastEvent?.type === 'snapshotChanged' && lastEvent.snapshot.requestId).toBe(13);
  });
});

describe('WebPlaybackBackend 事件隔离', () => {
  it('事件 ID 单调，且携带当前 sourceId/sourceEpoch/requestId', async () => {
    const video = createVideo();
    const backend = new WebPlaybackBackend(video);
    const events: PlaybackBackendEvent[] = [];
    backend.subscribe((event) => events.push(event));

    await backend.load(createSource('native', 'native'), createCommand('native', 7, 21));
    video.dispatchEvent(new Event('timeupdate'));
    video.dispatchEvent(new Event('playing'));

    expect(events.length).toBeGreaterThan(2);
    expect(
      events.every(
        (event) => event.sourceId === 'native' && event.sourceEpoch === 7 && event.requestId === 21,
      ),
    ).toBe(true);
    expect(events.map((event) => event.eventId)).toEqual(
      [...events.map((event) => event.eventId)].sort((left, right) => left - right),
    );
    expect(new Set(events.map((event) => event.eventId)).size).toBe(events.length);
  });

  it('真实 core 使用 facet 将稳定时间线逐帧命令映射为后端 seek', async () => {
    const video = createVideo();
    const frames = stubVideoFrameCallbacks(video);
    stubTimeline(video, 10);
    const backend = new WebPlaybackBackend(video);
    const seek = vi.spyOn(backend, 'seek');
    const core = new PlaybackCore({
      backend,
      facets: { framePresentation: backend.framePresentation },
      initialSeekTier: { count: 1, kind: 'frame' },
    });
    const playbackSource: PlaybackSource = {
      id: 'frames',
      mode: 'direct',
      payload: {
        frameTimeline: [
          { mediaTime: 1, sourceFrameIndex: 10 },
          { mediaTime: 1.04, sourceFrameIndex: 11 },
        ],
        kind: 'native',
        nominalFrameRate: 25,
        resolvePresentedFrameIdentity: (metadata) => ({
          sourceFrameIndex: metadata.mediaTime < 1.02 ? 10 : 11,
        }),
        url: '/frames.mp4',
      } satisfies WebPlaybackSourcePayload,
    };

    await core.load(playbackSource);
    video.currentTime = 1;
    video.dispatchEvent(new Event('progress'));
    frames.present(1);
    const stepping = core.seekByTier('next');
    await vi.waitFor(() => {
      expect(seek).toHaveBeenCalledWith(
        expect.objectContaining({ reason: 'step', targetTime: 1.04 }),
      );
    });
    frames.present(1.04);
    const result = await stepping;

    expect(result).toMatchObject({
      confirmedSourceFrameIndex: 11,
      precision: 'exact-verified',
      status: 'completed',
      targetSourceFrameIndex: 11,
    });
    core.dispose();
  });

  it('切源取消中的旧帧等待不发布 Network Error', async () => {
    const video = createVideo();
    const frames = stubVideoFrameCallbacks(video);
    stubTimeline(video, 10);
    const onPlaybackError = vi.fn();
    const backend = new WebPlaybackBackend(video, { onPlaybackError });
    const core = new PlaybackCore({
      backend,
      facets: { framePresentation: backend.framePresentation },
    });
    const exactSource: PlaybackSource = {
      id: 'old-frame-source',
      mode: 'direct',
      payload: {
        frameTimeline: [
          { mediaTime: 1, stableFrameId: 'old-a' },
          { mediaTime: 1.04, stableFrameId: 'old-b' },
        ],
        kind: 'native',
        resolvePresentedFrameIdentity: (metadata) => ({
          stableFrameId: metadata.mediaTime < 1.02 ? 'old-a' : 'old-b',
        }),
        url: '/old.mp4',
      } satisfies WebPlaybackSourcePayload,
    };

    await core.load(exactSource);
    video.currentTime = 1;
    video.dispatchEvent(new Event('progress'));
    frames.present(1);
    const stepping = core.stepFrame('next');
    await vi.waitFor(() => expect(video.currentTime).toBe(1.04));
    await core.load(createSource('new', 'native'));

    await expect(stepping).resolves.toMatchObject({ status: 'superseded' });
    expect(onPlaybackError).not.toHaveBeenCalled();
    expect(core.getSnapshot().error).toBeNull();
    core.dispose();
  });

  it('真实 core 丢弃旧播放完成，当前媒体事件使用暂停命令 requestId', async () => {
    const video = createVideo();
    const playGate = deferred<void>();
    Object.defineProperty(video, 'play', {
      configurable: true,
      value: vi.fn(() => playGate.promise),
    });
    const backend = new WebPlaybackBackend(video);
    const core = new PlaybackCore({ backend });

    await core.load(createSource('native', 'native'));
    const play = core.play();
    await flushMicrotasks();
    video.dispatchEvent(new Event('timeupdate'));
    expect(core.getSnapshot().requestId).toBe(2);

    await expect(core.pause()).resolves.toMatchObject({ requestId: 3, status: 'completed' });
    playGate.resolve();
    await expect(play).resolves.toMatchObject({ requestId: 2, status: 'superseded' });
    video.dispatchEvent(new Event('timeupdate'));

    expect(core.getSnapshot()).toMatchObject({ requestId: 3, state: 'paused' });
    core.dispose();
  });

  it('切源后旧内核回调无效', async () => {
    const video = createVideo();
    const oldPlayer = createMpegtsPlayer();
    mpegtsMock.createPlayer.mockReturnValueOnce(oldPlayer);
    const backend = new WebPlaybackBackend(video);
    const events: PlaybackBackendEvent[] = [];
    backend.subscribe((event) => events.push(event));

    const oldLoad = backend.load(createSource('old', 'mpegts'), createCommand('old', 1));
    await backend.load(createSource('new', 'native'), createCommand('new', 2));
    await oldLoad;
    const countAfterSwitch = events.length;
    oldPlayer.handlers.get('playing')?.();

    expect(events).toHaveLength(countAfterSwitch);
    expect(events.slice(-2).every((event) => event.sourceId === 'new')).toBe(true);
  });

  it('仅在预载轨道清单存在时暴露 tracks facet', async () => {
    const response: TrackResponse = {
      tracks: [],
      selection: {
        audio: { effectiveTrackId: null, selectedTrackId: null },
        subtitle: { effectiveTrackId: null, selectedTrackId: null },
      },
      backend: {},
      sources: {},
    };
    const backend = new WebPlaybackBackend(createVideo());

    await backend.load(createSource('without', 'native'), createCommand('without', 1));
    expect(backend.getSnapshot().capabilities.tracks).toBe('unavailable');

    await backend.load(
      createSource('with', 'native', '/with', { mediaId: 9, trackResponse: response }),
      createCommand('with', 2),
    );
    expect(backend.getSnapshot().capabilities.tracks).toBe('available');
    expect(backend.tracks.getTracks('subtitle')).toEqual([]);
  });

  it('dispose 幂等释放资源并屏蔽旧回调', async () => {
    const video = createVideo();
    const player = createMpegtsPlayer();
    mpegtsMock.createPlayer.mockReturnValue(player);
    const backend = new WebPlaybackBackend(video);
    const listener = vi.fn();
    backend.subscribe(listener);

    const loading = backend.load(createSource('ts', 'mpegts'), createCommand('ts', 1));
    backend.dispose();
    backend.dispose();
    await loading;
    const callCount = listener.mock.calls.length;
    player.handlers.get('playing')?.();

    expect(player.unload).toHaveBeenCalledOnce();
    expect(player.destroy).toHaveBeenCalledOnce();
    expect(listener).toHaveBeenCalledTimes(callCount);
    expect(backend.getSnapshot().state).toBe('disposed');
  });
});

describe('WebPlaybackBackend 恢复与错误', () => {
  it('真实 core 下 mpegts 暂时错误重载后恢复原 playing 意图', async () => {
    vi.useFakeTimers();
    const player = createMpegtsPlayer();
    mpegtsMock.createPlayer.mockReturnValue(player);
    const backend = new WebPlaybackBackend(createVideo());
    const core = new PlaybackCore({ backend });
    const loading = core.load(createSource('ts', 'mpegts'));
    await flushMicrotasks();
    player.handlers.get('loadeddata')?.();
    await loading;
    await core.play();

    player.handlers.get('error')?.();
    await vi.advanceTimersByTimeAsync(1000);
    player.handlers.get('loadeddata')?.();
    await flushMicrotasks();

    expect(player.load).toHaveBeenCalledTimes(2);
    expect(player.play).toHaveBeenCalledTimes(2);
    core.dispose();
  });

  it('mpegts 暂停态重载后不得误自动播放', async () => {
    vi.useFakeTimers();
    const player = createMpegtsPlayer();
    mpegtsMock.createPlayer.mockReturnValue(player);
    const backend = new WebPlaybackBackend(createVideo());
    const core = new PlaybackCore({ backend });
    const loading = core.load(createSource('ts', 'mpegts'));
    await flushMicrotasks();
    player.handlers.get('loadeddata')?.();
    await loading;
    await core.play();
    await core.pause();

    player.handlers.get('error')?.();
    await vi.advanceTimersByTimeAsync(1000);
    player.handlers.get('loadeddata')?.();
    await flushMicrotasks();

    expect(player.play).toHaveBeenCalledOnce();
    core.dispose();
  });

  it('HLS 就绪后的 fatal 也不把 m3u8 交给 mpegts', async () => {
    const video = createVideo();
    const backend = new WebPlaybackBackend(video);
    const core = new PlaybackCore({ backend });
    const loading = core.load(createSource('hls', 'hls', '/master.m3u8'));
    const hls = await waitForHlsInstance();
    completeHls(video, hls);
    await loading;
    await core.play();

    hls.handlers.get('error')?.('error', { fatal: true });
    await flushMicrotasks();

    expect(hls.destroy).toHaveBeenCalledOnce();
    expect(mpegtsMock.createPlayer).not.toHaveBeenCalled();
    core.dispose();
  });

  it('HLS 暂停态 fatal 不创建 mpegts player', async () => {
    const video = createVideo();
    const backend = new WebPlaybackBackend(video);
    const core = new PlaybackCore({ backend });
    const loading = core.load(createSource('hls', 'hls', '/master.m3u8'));
    const hls = await waitForHlsInstance();
    completeHls(video, hls);
    await loading;
    await core.play();
    await core.pause();

    hls.handlers.get('error')?.('error', { fatal: true });
    await flushMicrotasks();

    expect(mpegtsMock.createPlayer).not.toHaveBeenCalled();
    core.dispose();
  });

  it('原生错误按 NETWORK/DECODE/SRC_NOT_SUPPORTED/其他 media 分类', async () => {
    const video = createVideo();
    const onPlaybackError = vi.fn();
    const backend = new WebPlaybackBackend(video, { onPlaybackError });
    await backend.load(createSource('native', 'native'), createCommand('native', 1));

    for (const [code, category] of [
      [2, 'network'],
      [3, 'decode'],
      [4, 'unsupported'],
      [1, 'media'],
    ] as const) {
      Object.defineProperty(video, 'error', { configurable: true, get: () => ({ code }) });
      video.dispatchEvent(new Event('error'));
      expect(onPlaybackError).toHaveBeenLastCalledWith(expect.objectContaining({ category }));
    }
  });
});
