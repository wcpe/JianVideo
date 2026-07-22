import { PlaybackCore } from '@jianvideo/player-core';
import type {
  FrameStepResult,
  PlaybackCompletionStatus,
  WatchStateReport,
  WatchStateTransport,
} from '@jianvideo/player-core';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import VideoPlayer from './VideoPlayer';
import { WebPlaybackBackend } from '@/player/WebPlaybackBackend';
import {
  DEFAULT_PLAYER_VISUAL_BRIGHTNESS,
  WebMediaSessionAdapter,
  WebPictureInPictureAdapter,
  classifyPlayerGesture,
  detectWebPlayerCapabilities,
  isGestureInteractiveTarget,
  mapSeekGesture,
  mapVerticalGesture,
} from '@/player/WebPlatformAdapter';

vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => ({
  default: class {
    static isSupported() {
      return false;
    }
  },
}));

function stubMedia() {
  const prototype = Object.getPrototypeOf(
    Object.getPrototypeOf(document.createElement('video')),
  ) as HTMLMediaElement;
  Object.defineProperties(prototype, {
    load: { configurable: true, value: vi.fn(), writable: true },
    pause: { configurable: true, value: vi.fn(), writable: true },
    play: { configurable: true, value: vi.fn(() => Promise.resolve()), writable: true },
  });
}

function stubTimeline(video: HTMLVideoElement, duration = 100) {
  let currentTime = 40;
  Object.defineProperties(video, {
    currentTime: {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    },
    duration: { configurable: true, get: () => duration },
    seekable: {
      configurable: true,
      get: () => ({ end: () => duration, length: 1, start: () => 0 }),
    },
  });
}

class FakeMediaSession {
  readonly handlers = new Map<
    string,
    ((details?: { seekOffset?: number; seekTime?: number }) => void) | null
  >();
  metadata: unknown = null;
  playbackState: MediaSessionPlaybackState = 'none';
  readonly positionStates: Array<MediaPositionState | undefined> = [];

  setActionHandler(
    action: MediaSessionAction,
    handler: ((details?: { seekOffset?: number; seekTime?: number }) => void) | null,
  ) {
    this.handlers.set(action, handler);
  }

  setPositionState(state?: MediaPositionState) {
    this.positionStates.push(state);
  }
}

function deferred<T>() {
  let reject!: (reason?: unknown) => void;
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((settle, fail) => {
    reject = fail;
    resolve = settle;
  });
  return { promise, reject, resolve };
}

function frameStepResult(status: PlaybackCompletionStatus): FrameStepResult {
  return {
    clamped: false,
    confirmedMediaTime: 0,
    correctionCount: 0,
    direction: 'next',
    frameDuration: null,
    precision: status === 'unsupported' ? 'unsupported' : 'approximate',
    requestId: 2,
    startMediaTime: 0,
    status,
    targetMediaTime: 0,
    timestampError: null,
  };
}

function createWatchTransport(): {
  readonly send: ReturnType<typeof vi.fn<WatchStateTransport['send']>>;
  readonly transport: WatchStateTransport;
} {
  let revision = 0;
  const send = vi.fn<WatchStateTransport['send']>((event) => {
    revision += 1;
    return Promise.resolve({
      applied: true,
      current: { completed: false, positionSeconds: event.positionSeconds, revision },
      kind: 'applied',
    });
  });
  return { send, transport: { send } };
}

function renderPlayer(props: Partial<React.ComponentProps<typeof VideoPlayer>> = {}) {
  return render(
    <MantineProvider>
      <VideoPlayer
        autoPlay={false}
        mediaTitle="测试视频"
        poster="/api/library/thumbnail/1"
        streamType="mp4"
        url="/video-a.mp4"
        {...props}
      />
    </MantineProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
  stubMedia();
  Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: undefined });
  Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: false });
  Object.defineProperty(document, 'pictureInPictureElement', {
    configurable: true,
    value: null,
    writable: true,
  });
  Object.defineProperty(document, 'exitPictureInPicture', {
    configurable: true,
    value: undefined,
    writable: true,
  });
  Object.defineProperty(
    Object.getPrototypeOf(document.createElement('video')),
    'requestPictureInPicture',
    {
      configurable: true,
      value: undefined,
      writable: true,
    },
  );
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('FR2-058 平台能力与手势纯逻辑', () => {
  it('能力模型不把后台音频或播放器视觉亮度虚报为系统能力', () => {
    const video = document.createElement('video');
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(video, 'requestPictureInPicture', { configurable: true, value: vi.fn() });

    expect(detectWebPlayerCapabilities(video, {})).toEqual({
      backgroundAudio: 'best-effort',
      mediaSession: 'available',
      pictureInPicture: 'available',
      playerVisualBrightness: 'available',
      systemBrightness: 'unsupported',
      touchSeek: 'available',
      touchVolume: 'available',
    });
  });

  it('超过阈值后锁定横滑、右侧音量和左侧视觉亮度', () => {
    expect(
      classifyPlayerGesture({ deltaX: 8, deltaY: 2, startX: 20, threshold: 12, width: 200 }),
    ).toBeNull();
    expect(
      classifyPlayerGesture({ deltaX: 40, deltaY: 5, startX: 20, threshold: 12, width: 200 }),
    ).toBe('seek');
    expect(
      classifyPlayerGesture({ deltaX: 2, deltaY: 40, startX: 160, threshold: 12, width: 200 }),
    ).toBe('volume');
    expect(
      classifyPlayerGesture({ deltaX: 2, deltaY: 40, startX: 40, threshold: 12, width: 200 }),
    ).toBe('brightness');
  });

  it('seek、音量和视觉亮度映射均夹取合法范围', () => {
    expect(mapSeekGesture({ deltaX: 300, duration: 100, startTime: 40, width: 200 })).toBe(100);
    expect(mapSeekGesture({ deltaX: -300, duration: 100, startTime: 40, width: 200 })).toBe(0);
    expect(mapVerticalGesture({ deltaY: -100, height: 100, max: 1, min: 0, startValue: 0.4 })).toBe(
      1,
    );
    expect(
      mapVerticalGesture({ deltaY: 100, height: 100, max: 1.5, min: 0.5, startValue: 1 }),
    ).toBe(0.5);
  });

  it('按钮、输入与进度条热区不会启动播放器面手势', () => {
    const surface = document.createElement('div');
    const button = document.createElement('button');
    const progress = document.createElement('div');
    progress.dataset.playerGestureIgnore = 'true';
    surface.append(button, progress);

    expect(isGestureInteractiveTarget(button, surface)).toBe(true);
    expect(isGestureInteractiveTarget(progress, surface)).toBe(true);
    expect(isGestureInteractiveTarget(surface, surface)).toBe(false);
  });
});

describe('FR2-058 PiP 适配器', () => {
  it('切源退出的 Promise resolve 后仍等待 leavepictureinpicture 收敛 idle', async () => {
    const video = document.createElement('video');
    let resolveExit: (() => void) | undefined;
    const exit = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveExit = resolve;
        }),
    );
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      value: video,
      writable: true,
    });
    Object.defineProperty(document, 'exitPictureInPicture', {
      configurable: true,
      value: exit,
      writable: true,
    });
    Object.defineProperty(video, 'requestPictureInPicture', {
      configurable: true,
      value: vi.fn(() => Promise.resolve({})),
    });
    const states: string[] = [];
    const adapter = new WebPictureInPictureAdapter(
      video,
      document,
      (state) => states.push(state),
      vi.fn(),
    );
    video.dispatchEvent(new Event('enterpictureinpicture'));

    const reset = adapter.resetForSourceChange();
    expect(exit).toHaveBeenCalledOnce();
    expect(states.at(-1)).toBe('exiting');

    resolveExit?.();
    await reset;
    expect(states.at(-1)).toBe('exiting');

    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      value: null,
      writable: true,
    });
    video.dispatchEvent(new Event('leavepictureinpicture'));
    expect(states.at(-1)).toBe('idle');
    adapter.dispose();
  });

  it('进入请求 pending 时切源，旧请求迟到进入后立即退出且不回写 active', async () => {
    const video = document.createElement('video');
    const request = deferred<unknown>();
    let pipElement: Element | null = null;
    const exit = vi.fn(async () => {
      pipElement = null;
      video.dispatchEvent(new Event('leavepictureinpicture'));
    });
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      get: () => pipElement,
    });
    Object.defineProperty(document, 'exitPictureInPicture', {
      configurable: true,
      value: exit,
    });
    Object.defineProperty(video, 'requestPictureInPicture', {
      configurable: true,
      value: vi.fn(() => request.promise),
    });
    const states: string[] = [];
    const adapter = new WebPictureInPictureAdapter(
      video,
      document,
      (state) => states.push(state),
      vi.fn(),
    );

    const toggling = adapter.toggle();
    await Promise.resolve();
    expect(adapter.getState()).toBe('requesting');
    await adapter.resetForSourceChange();
    const resetStateCount = states.length;

    pipElement = video;
    video.dispatchEvent(new Event('enterpictureinpicture'));
    request.resolve({});
    await toggling;
    await Promise.resolve();

    expect(exit).toHaveBeenCalledOnce();
    expect(adapter.getState()).toBe('idle');
    expect(states.slice(resetStateCount)).not.toContain('active');
    adapter.dispose();
  });

  it.each(['resolve', 'reject'] as const)(
    '旧进入请求切源失效后，新请求已进入时旧请求迟到 %s 不得退出新会话',
    async (settlement) => {
      const video = document.createElement('video');
      const oldRequest = deferred<unknown>();
      const newRequest = deferred<unknown>();
      let pipElement: Element | null = null;
      const exit = vi.fn(async () => {
        pipElement = null;
        video.dispatchEvent(new Event('leavepictureinpicture'));
      });
      const request = vi
        .fn()
        .mockImplementationOnce(() => oldRequest.promise)
        .mockImplementationOnce(() => newRequest.promise);
      Object.defineProperty(document, 'pictureInPictureEnabled', {
        configurable: true,
        value: true,
      });
      Object.defineProperty(document, 'pictureInPictureElement', {
        configurable: true,
        get: () => pipElement,
      });
      Object.defineProperty(document, 'exitPictureInPicture', {
        configurable: true,
        value: exit,
      });
      Object.defineProperty(video, 'requestPictureInPicture', {
        configurable: true,
        value: request,
      });
      const adapter = new WebPictureInPictureAdapter(video, document, vi.fn(), vi.fn());

      const oldToggle = adapter.toggle();
      await Promise.resolve();
      await adapter.resetForSourceChange();
      const newToggle = adapter.toggle();
      await Promise.resolve();
      expect(request).toHaveBeenCalledTimes(2);
      expect(adapter.getState()).toBe('requesting');

      pipElement = video;
      video.dispatchEvent(new Event('enterpictureinpicture'));
      expect(adapter.getState()).toBe('active');
      if (settlement === 'resolve') oldRequest.resolve({});
      else oldRequest.reject(new Error('旧请求迟到失败'));
      await oldToggle;
      expect(exit).not.toHaveBeenCalled();
      expect(adapter.getState()).toBe('active');

      newRequest.resolve({});
      await newToggle;
      expect(exit).not.toHaveBeenCalled();
      expect(adapter.getState()).toBe('active');
      adapter.dispose();
    },
  );

  it('退出 pending 时重复切源复用同一次退出请求', async () => {
    const video = document.createElement('video');
    const exitPending = deferred<void>();
    let pipElement: Element | null = video;
    const exit = vi.fn(() => exitPending.promise);
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      get: () => pipElement,
    });
    Object.defineProperty(document, 'exitPictureInPicture', {
      configurable: true,
      value: exit,
    });
    Object.defineProperty(video, 'requestPictureInPicture', {
      configurable: true,
      value: vi.fn(() => Promise.resolve({})),
    });
    const adapter = new WebPictureInPictureAdapter(video, document, vi.fn(), vi.fn());
    video.dispatchEvent(new Event('enterpictureinpicture'));

    const firstReset = adapter.resetForSourceChange();
    const secondReset = adapter.resetForSourceChange();
    expect(exit).toHaveBeenCalledOnce();
    expect(adapter.getState()).toBe('exiting');

    pipElement = null;
    video.dispatchEvent(new Event('leavepictureinpicture'));
    exitPending.resolve();
    await Promise.all([firstReset, secondReset]);
    expect(adapter.getState()).toBe('idle');
    adapter.dispose();
  });

  it('旧会话退出 pending 期间新会话进入后，后续切源必须为新会话发起第二次退出', async () => {
    const video = document.createElement('video');
    const firstExit = deferred<void>();
    const secondExit = deferred<void>();
    let pipElement: Element | null = video;
    const exit = vi
      .fn()
      .mockImplementationOnce(() => firstExit.promise)
      .mockImplementationOnce(() => secondExit.promise);
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      get: () => pipElement,
    });
    Object.defineProperty(document, 'exitPictureInPicture', {
      configurable: true,
      value: exit,
    });
    Object.defineProperty(video, 'requestPictureInPicture', {
      configurable: true,
      value: vi.fn(() => Promise.resolve({})),
    });
    const adapter = new WebPictureInPictureAdapter(video, document, vi.fn(), vi.fn());
    video.dispatchEvent(new Event('enterpictureinpicture'));

    const oldReset = adapter.resetForSourceChange();
    expect(exit).toHaveBeenCalledOnce();
    pipElement = null;
    video.dispatchEvent(new Event('leavepictureinpicture'));
    pipElement = video;
    video.dispatchEvent(new Event('enterpictureinpicture'));
    const newReset = adapter.resetForSourceChange();

    firstExit.resolve();
    await oldReset;
    await waitFor(() => expect(exit).toHaveBeenCalledTimes(2));
    expect(adapter.getState()).toBe('exiting');

    pipElement = null;
    video.dispatchEvent(new Event('leavepictureinpicture'));
    secondExit.resolve();
    await newReset;
    expect(adapter.getState()).toBe('idle');
    adapter.dispose();
  });

  it('退出 pending 时 dispose 不重复退出且不再回写状态', async () => {
    const video = document.createElement('video');
    const exitPending = deferred<void>();
    const exit = vi.fn(() => exitPending.promise);
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      value: video,
      writable: true,
    });
    Object.defineProperty(document, 'exitPictureInPicture', {
      configurable: true,
      value: exit,
    });
    Object.defineProperty(video, 'requestPictureInPicture', {
      configurable: true,
      value: vi.fn(() => Promise.resolve({})),
    });
    const states: string[] = [];
    const adapter = new WebPictureInPictureAdapter(
      video,
      document,
      (state) => states.push(state),
      vi.fn(),
    );
    video.dispatchEvent(new Event('enterpictureinpicture'));

    const reset = adapter.resetForSourceChange();
    expect(exit).toHaveBeenCalledOnce();
    adapter.dispose();
    const disposedStateCount = states.length;
    expect(exit).toHaveBeenCalledOnce();

    exitPending.resolve();
    await reset;
    expect(adapter.getState()).toBe('unsupported');
    expect(states).toHaveLength(disposedStateCount);
  });

  it('幂等退出拒绝后清空 in-flight，后续重试可收敛 idle', async () => {
    const video = document.createElement('video');
    const firstExit = deferred<void>();
    let pipElement: Element | null = video;
    const exit = vi
      .fn()
      .mockImplementationOnce(() => firstExit.promise)
      .mockImplementationOnce(async () => {
        pipElement = null;
        video.dispatchEvent(new Event('leavepictureinpicture'));
      });
    const onError = vi.fn();
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      get: () => pipElement,
    });
    Object.defineProperty(document, 'exitPictureInPicture', {
      configurable: true,
      value: exit,
    });
    Object.defineProperty(video, 'requestPictureInPicture', {
      configurable: true,
      value: vi.fn(() => Promise.resolve({})),
    });
    const adapter = new WebPictureInPictureAdapter(video, document, vi.fn(), onError);
    video.dispatchEvent(new Event('enterpictureinpicture'));

    const firstReset = adapter.resetForSourceChange();
    const duplicateReset = adapter.resetForSourceChange();
    expect(exit).toHaveBeenCalledOnce();
    firstExit.reject(new Error('首次退出失败'));
    await Promise.all([firstReset, duplicateReset]);
    expect(adapter.getState()).toBe('error');
    expect(onError).toHaveBeenCalledOnce();

    await adapter.resetForSourceChange();
    expect(exit).toHaveBeenCalledTimes(2);
    expect(adapter.getState()).toBe('idle');
    adapter.dispose();
  });

  it('进入请求 pending 时卸载，旧请求迟到进入后退出且不再回写状态', async () => {
    const video = document.createElement('video');
    const request = deferred<unknown>();
    let pipElement: Element | null = null;
    const exit = vi.fn(async () => {
      pipElement = null;
    });
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      get: () => pipElement,
    });
    Object.defineProperty(document, 'exitPictureInPicture', {
      configurable: true,
      value: exit,
    });
    Object.defineProperty(video, 'requestPictureInPicture', {
      configurable: true,
      value: vi.fn(() => request.promise),
    });
    const states: string[] = [];
    const adapter = new WebPictureInPictureAdapter(
      video,
      document,
      (state) => states.push(state),
      vi.fn(),
    );

    const toggling = adapter.toggle();
    await Promise.resolve();
    adapter.dispose();
    const disposedStateCount = states.length;

    pipElement = video;
    video.dispatchEvent(new Event('enterpictureinpicture'));
    request.resolve({});
    await toggling;
    await Promise.resolve();

    expect(exit).toHaveBeenCalledOnce();
    expect(adapter.getState()).toBe('unsupported');
    expect(states).toHaveLength(disposedStateCount);
  });
});

describe('FR2-058 Media Session 适配器', () => {
  it('action handlers 统一映射核心命令并在销毁时清理', () => {
    const session = new FakeMediaSession();
    const commands = {
      pause: vi.fn(),
      play: vi.fn(),
      seekBy: vi.fn(),
      seekTo: vi.fn(),
      stop: vi.fn(),
    };
    const adapter = new WebMediaSessionAdapter(session, commands, (metadata) => metadata);

    adapter.setMetadata({ applicationName: 'JianVideo', artwork: '/cover.jpg', title: '测试视频' });
    session.handlers.get('play')?.();
    session.handlers.get('pause')?.();
    session.handlers.get('seekbackward')?.({ seekOffset: 7 });
    session.handlers.get('seekforward')?.({ seekOffset: 12 });
    session.handlers.get('seekto')?.({ seekTime: 33 });
    session.handlers.get('stop')?.();

    expect(commands.play).toHaveBeenCalledOnce();
    expect(commands.pause).toHaveBeenCalledOnce();
    expect(commands.seekBy).toHaveBeenNthCalledWith(1, -7);
    expect(commands.seekBy).toHaveBeenNthCalledWith(2, 12);
    expect(commands.seekTo).toHaveBeenCalledWith(33);
    expect(commands.stop).toHaveBeenCalledOnce();
    expect(session.metadata).toEqual({
      album: 'JianVideo',
      artist: 'JianVideo',
      artwork: [{ src: '/cover.jpg' }],
      title: '测试视频',
    });

    adapter.dispose();
    expect([...session.handlers.values()].every((handler) => handler === null)).toBe(true);
    expect(session.metadata).toBeNull();
    expect(session.playbackState).toBe('none');
  });

  it('只在时长、位置和倍速均合法时同步 position state', () => {
    const session = new FakeMediaSession();
    const adapter = new WebMediaSessionAdapter(
      session,
      { pause: vi.fn(), play: vi.fn(), seekBy: vi.fn(), seekTo: vi.fn(), stop: vi.fn() },
      (metadata) => metadata,
    );

    adapter.sync({ currentTime: 30, duration: 100, playbackRate: 1.25, state: 'playing' });
    adapter.sync({
      currentTime: 0,
      duration: Number.POSITIVE_INFINITY,
      playbackRate: 1,
      state: 'paused',
    });

    expect(session.playbackState).toBe('paused');
    expect(session.positionStates).toEqual([
      { duration: 100, playbackRate: 1.25, position: 30 },
      undefined,
    ]);
  });
});

describe('FR2-058 VideoPlayer 平台接线', () => {
  it('注册 Media Session，映射 stop/seek 并在卸载后清理', async () => {
    const session = new FakeMediaSession();
    Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
    vi.stubGlobal(
      'MediaMetadata',
      class {
        constructor(value: object) {
          Object.assign(this, value);
        }
      },
    );
    const stop = vi
      .spyOn(PlaybackCore.prototype, 'stop')
      .mockResolvedValue({ requestId: 1, status: 'completed' });
    const seekBy = vi.spyOn(PlaybackCore.prototype, 'seekBy').mockResolvedValue({
      clamped: false,
      confirmedTime: 30,
      requestId: 1,
      requestedTime: 30,
      status: 'completed',
      targetTime: 30,
    });
    const view = renderPlayer();

    await waitFor(() => expect(session.handlers.get('stop')).toEqual(expect.any(Function)));
    act(() => session.handlers.get('seekforward')?.({ seekOffset: 15 }));
    act(() => session.handlers.get('stop')?.());
    expect(seekBy).toHaveBeenCalledWith(15);
    expect(stop).toHaveBeenCalledOnce();

    view.unmount();
    expect([...session.handlers.values()].every((handler) => handler === null)).toBe(true);
  });

  it('按钮 pause→play 后同媒体切源保持最终播放意图', async () => {
    const view = renderPlayer({ autoPlay: true, mediaId: 9 });
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(video.play).toHaveBeenCalledOnce());
    fireEvent.click(await screen.findByRole('button', { name: '暂停' }));
    await screen.findByRole('button', { name: '播放' });
    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    await waitFor(() => expect(video.play).toHaveBeenCalledTimes(2));
    await screen.findByRole('button', { name: '暂停' });

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay
          mediaId={9}
          mediaTitle="同媒体新播放源"
          streamType="mp4"
          url="/video-b.mp4"
        />
      </MantineProvider>,
    );

    await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
    await waitFor(() => expect(video.play).toHaveBeenCalledTimes(3));
  });

  it('同媒体切源 loading 期间 pause→play 后按最新 playing 意图收敛', async () => {
    const session = new FakeMediaSession();
    const pendingLoad = deferred<void>();
    const play = vi.spyOn(PlaybackCore.prototype, 'play');
    const originalLoad = WebPlaybackBackend.prototype.load;
    let targetLoadStarted = false;
    vi.spyOn(WebPlaybackBackend.prototype, 'load').mockImplementation(async function (
      this: WebPlaybackBackend,
      ...args
    ) {
      await originalLoad.apply(this, args);
      if (args[0].id.includes('/video-b.mp4')) {
        targetLoadStarted = true;
        await pendingLoad.promise;
      }
    });
    Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
    const view = renderPlayer({ autoPlay: true, mediaId: 9 });
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(video.play).toHaveBeenCalledOnce());
    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay
          mediaId={9}
          mediaTitle="同媒体加载中播放源"
          streamType="mp4"
          url="/video-b.mp4"
        />
      </MantineProvider>,
    );
    await waitFor(() => expect(targetLoadStarted).toBe(true));
    const playCountBeforeIntent = play.mock.calls.length;
    act(() => session.handlers.get('pause')?.());
    act(() => session.handlers.get('play')?.());
    await waitFor(() => expect(play).toHaveBeenCalledTimes(playCountBeforeIntent + 2));
    const reconciledPlayCount = play.mock.calls.length;

    pendingLoad.resolve();
    await act(async () => Promise.resolve());
    expect(play).toHaveBeenCalledTimes(reconciledPlayCount);
  });

  it('frame-tier 首次 unsupported 后遇到新 Play 与切源时不得重试旧命令', async () => {
    const session = new FakeMediaSession();
    const firstResult = deferred<Awaited<ReturnType<PlaybackCore['seekByTier']>>>();
    const seekByTier = vi
      .spyOn(PlaybackCore.prototype, 'seekByTier')
      .mockReturnValueOnce(firstResult.promise)
      .mockResolvedValue({ requestId: 4, status: 'completed' });
    Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
    const view = renderPlayer({ autoPlay: true, mediaId: 9 });
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(video.play).toHaveBeenCalledOnce());
    fireEvent.click(screen.getByRole('button', { name: '定位档位：5 秒' }));
    fireEvent.click(await screen.findByRole('menuitem', { name: '1 帧' }));
    fireEvent.click(await screen.findByRole('button', { name: '前进 1 帧' }));
    await waitFor(() => expect(seekByTier).toHaveBeenCalledOnce());

    act(() => session.handlers.get('play')?.());
    await waitFor(() => expect(video.play).toHaveBeenCalledTimes(2));
    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay
          mediaId={9}
          mediaTitle="新播放意图对应的新源"
          streamType="mp4"
          url="/video-b.mp4"
        />
      </MantineProvider>,
    );
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
    await waitFor(() => expect(video.play).toHaveBeenCalledTimes(3));
    const pauseCount = vi.mocked(video.pause).mock.calls.length;

    firstResult.resolve({ requestId: 3, status: 'unsupported' });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(seekByTier).toHaveBeenCalledOnce();
    expect(video.pause).toHaveBeenCalledTimes(pauseCount);
    expect(session.playbackState).toBe('playing');
  });

  it('Media Session pause→play 后同媒体切源保持最终播放意图', async () => {
    const session = new FakeMediaSession();
    Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
    const view = renderPlayer({ autoPlay: true, mediaId: 9 });
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(session.handlers.get('pause')).toEqual(expect.any(Function)));
    await waitFor(() => expect(video.play).toHaveBeenCalledOnce());
    act(() => session.handlers.get('pause')?.());
    await waitFor(() => expect(session.playbackState).toBe('paused'));
    act(() => session.handlers.get('play')?.());
    await waitFor(() => expect(video.play).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(session.playbackState).toBe('playing'));

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay
          mediaId={9}
          mediaTitle="同媒体新播放源"
          streamType="mp4"
          url="/video-b.mp4"
        />
      </MantineProvider>,
    );

    await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
    await waitFor(() => expect(video.play).toHaveBeenCalledTimes(3));
  });

  it('Media Session pause 作为用户意图上报并阻止同媒体切源自动播放', async () => {
    const session = new FakeMediaSession();
    const { send, transport } = createWatchTransport();
    const watchState = { completed: false, positionSeconds: 0, revision: 0 } as const;
    Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
    const view = renderPlayer({
      autoPlay: true,
      mediaId: 9,
      watchContextKey: 'space-default:9',
      watchState,
      watchStateTransport: transport,
    });
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(session.handlers.get('pause')).toEqual(expect.any(Function)));
    await waitFor(() => expect(video.play).toHaveBeenCalledOnce());
    act(() => session.handlers.get('pause')?.());
    await act(async () => Promise.resolve());
    act(() => video.dispatchEvent(new Event('pause')));

    await waitFor(() =>
      expect(send.mock.calls.map(([report]) => report satisfies WatchStateReport)).toContainEqual(
        expect.objectContaining({ eventType: 'pause', reason: 'user' }),
      ),
    );

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay
          mediaId={9}
          mediaTitle="下一条播放源"
          streamType="mp4"
          url="/video-b.mp4"
          watchContextKey="space-default:9"
          watchState={watchState}
          watchStateTransport={transport}
        />
      </MantineProvider>,
    );
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
    await act(async () => Promise.resolve());

    expect(video.play).toHaveBeenCalledOnce();
  });

  it.each(['stop', 'frame-step'] as const)(
    '%s 用户动作建立 paused 意图并阻止同媒体切源自动播放',
    async (action) => {
      const session = new FakeMediaSession();
      Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
      const view = renderPlayer({ autoPlay: true, mediaId: 9 });
      const video = view.container.querySelector('video')!;

      await waitFor(() => expect(video.play).toHaveBeenCalledOnce());
      if (action === 'stop') {
        await waitFor(() => expect(session.handlers.get('stop')).toEqual(expect.any(Function)));
        act(() => session.handlers.get('stop')?.());
      } else {
        fireEvent.click(await screen.findByRole('button', { name: '后一帧' }));
      }

      view.rerender(
        <MantineProvider>
          <VideoPlayer
            autoPlay
            mediaId={9}
            mediaTitle="同媒体动作后新播放源"
            streamType="mp4"
            url="/video-b.mp4"
          />
        </MantineProvider>,
      );
      await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
      await act(async () => Promise.resolve());
      expect(video.play).toHaveBeenCalledOnce();
    },
  );

  it('1 帧定位档按钮建立 paused 意图并阻止同媒体切源自动播放', async () => {
    const view = renderPlayer({ autoPlay: true, mediaId: 9 });
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(video.play).toHaveBeenCalledOnce());
    fireEvent.click(screen.getByRole('button', { name: '定位档位：5 秒' }));
    fireEvent.click(await screen.findByRole('menuitem', { name: '1 帧' }));
    await screen.findByRole('button', { name: '定位档位：1 帧' });
    fireEvent.click(screen.getByRole('button', { name: '前进 1 帧' }));

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay
          mediaId={9}
          mediaTitle="同媒体逐帧档后新播放源"
          streamType="mp4"
          url="/video-b.mp4"
        />
      </MantineProvider>,
    );
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
    await act(async () => Promise.resolve());

    expect(video.play).toHaveBeenCalledOnce();
  });

  it.each(['failed', 'unsupported', 'superseded'] as const)(
    'manual play 返回 %s 时回滚暂停意图且同媒体切源不自动重试',
    async (status) => {
      const play = vi
        .spyOn(PlaybackCore.prototype, 'play')
        .mockResolvedValue({ requestId: 2, status });
      const view = renderPlayer({ autoPlay: false, mediaId: 9 });
      const video = view.container.querySelector('video')!;
      await waitFor(() => expect(video.getAttribute('src')).toBe('/video-a.mp4'));

      fireEvent.click(screen.getByRole('button', { name: '播放' }));
      await waitFor(() => expect(play).toHaveBeenCalledOnce());
      await act(async () => Promise.resolve());

      view.rerender(
        <MantineProvider>
          <VideoPlayer
            autoPlay
            mediaId={9}
            mediaTitle="播放失败后的同媒体新源"
            streamType="mp4"
            url="/video-b.mp4"
          />
        </MantineProvider>,
      );
      await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
      await act(async () => Promise.resolve());

      expect(play).toHaveBeenCalledOnce();
    },
  );

  it.each([
    ['pause', 'unsupported'],
    ['stop', 'failed'],
    ['frame-step', 'superseded'],
    ['frame-tier', 'unsupported'],
  ] as const)('%s 返回 %s 且未产生原生 pause 时不遗留 paused 意图', async (action, status) => {
    const session = new FakeMediaSession();
    Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
    const play = vi.spyOn(PlaybackCore.prototype, 'play');
    const command =
      action === 'pause' || action === 'stop'
        ? vi.spyOn(PlaybackCore.prototype, action).mockResolvedValue({ requestId: 2, status })
        : action === 'frame-step'
          ? vi.spyOn(PlaybackCore.prototype, 'stepFrame').mockResolvedValue(frameStepResult(status))
          : vi
              .spyOn(PlaybackCore.prototype, 'seekByTier')
              .mockResolvedValue({ requestId: 2, status });
    const view = renderPlayer({ autoPlay: true, mediaId: 9 });
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(play).toHaveBeenCalledOnce());
    if (action === 'pause' || action === 'stop') {
      await waitFor(() => expect(session.handlers.get(action)).toEqual(expect.any(Function)));
      act(() => session.handlers.get(action)?.());
    } else if (action === 'frame-step') {
      fireEvent.click(await screen.findByRole('button', { name: '后一帧' }));
    } else {
      fireEvent.click(screen.getByRole('button', { name: '定位档位：5 秒' }));
      fireEvent.click(await screen.findByRole('menuitem', { name: '1 帧' }));
      fireEvent.click(await screen.findByRole('button', { name: '前进 1 帧' }));
    }
    await waitFor(() => expect(command).toHaveBeenCalledTimes(action === 'frame-tier' ? 2 : 1));

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay
          mediaId={9}
          mediaTitle="失败动作后的同媒体新源"
          streamType="mp4"
          url="/video-b.mp4"
        />
      </MantineProvider>,
    );
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
    await waitFor(() => expect(play).toHaveBeenCalledTimes(2));
  });

  it('秒级定位档不建立 paused 意图，同媒体切源仍按 autoPlay 播放', async () => {
    const play = vi.spyOn(PlaybackCore.prototype, 'play');
    const view = renderPlayer({ autoPlay: true, mediaId: 9 });
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(play).toHaveBeenCalledOnce());
    fireEvent.click(screen.getByRole('button', { name: '前进 5 秒' }));

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay
          mediaId={9}
          mediaTitle="同媒体秒级定位后新播放源"
          streamType="mp4"
          url="/video-b.mp4"
        />
      </MantineProvider>,
    );
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video-b.mp4'));
    await waitFor(() => expect(play).toHaveBeenCalledTimes(2));
  });

  it('PiP 只调用当前 video 的原生 API，并由真实事件同步按钮状态', async () => {
    const request = vi.fn(() => Promise.resolve({}));
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, value: true });
    Object.defineProperty(document, 'pictureInPictureElement', {
      configurable: true,
      value: null,
      writable: true,
    });
    Object.defineProperty(
      Object.getPrototypeOf(document.createElement('video')),
      'requestPictureInPicture',
      {
        configurable: true,
        value: request,
        writable: true,
      },
    );
    const view = renderPlayer();
    const video = view.container.querySelector('video')!;

    await waitFor(() => expect(screen.getByRole('button', { name: '画中画' })).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: '画中画' }));
    expect(request).toHaveBeenCalledOnce();
    act(() => video.dispatchEvent(new Event('enterpictureinpicture')));
    expect(screen.getByRole('button', { name: '退出画中画' })).toBeInTheDocument();
    act(() => video.dispatchEvent(new Event('leavepictureinpicture')));
    expect(screen.getByRole('button', { name: '画中画' })).toBeInTheDocument();
  });

  it('横滑仅抬手提交，取消、多指与失捕获恢复预览，右纵滑实时调整音量', async () => {
    const seek = vi.spyOn(PlaybackCore.prototype, 'seek').mockResolvedValue({
      clamped: false,
      confirmedTime: 100,
      requestId: 1,
      requestedTime: 100,
      status: 'completed',
      targetTime: 100,
    });
    const view = renderPlayer();
    const video = view.container.querySelector('video')!;
    stubTimeline(video);
    act(() => video.dispatchEvent(new Event('timeupdate')));
    let mediaVolume = video.volume;
    let mediaMuted = video.muted;
    Object.defineProperties(video, {
      muted: {
        configurable: true,
        get: () => mediaMuted,
        set: (value: boolean) => {
          mediaMuted = value;
        },
      },
      volume: {
        configurable: true,
        get: () => mediaVolume,
        set: (value: number) => {
          mediaVolume = value;
        },
      },
    });
    const surface = await screen.findByTestId('video-gesture-surface');
    expect(surface).toHaveStyle({ touchAction: 'none' });
    vi.spyOn(surface, 'getBoundingClientRect').mockReturnValue({
      bottom: 100,
      height: 100,
      left: 0,
      right: 200,
      toJSON: () => ({}),
      top: 0,
      width: 200,
      x: 0,
      y: 0,
    });
    await waitFor(() =>
      expect(screen.getByTestId('video-current-time')).toHaveTextContent('0:40.000'),
    );

    fireEvent.pointerDown(surface, {
      clientX: 40,
      clientY: 50,
      pointerId: 1,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(surface, {
      clientX: 140,
      clientY: 54,
      pointerId: 1,
      pointerType: 'touch',
    });
    expect(seek).not.toHaveBeenCalled();
    fireEvent.pointerUp(surface, { clientX: 140, clientY: 54, pointerId: 1, pointerType: 'touch' });
    expect(seek).toHaveBeenCalledOnce();

    fireEvent.pointerDown(surface, {
      clientX: 40,
      clientY: 50,
      pointerId: 4,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(surface, {
      clientX: 140,
      clientY: 54,
      pointerId: 4,
      pointerType: 'touch',
    });
    expect(screen.getByTestId('video-current-time')).toHaveTextContent('1:40.000');
    fireEvent.pointerDown(surface, {
      clientX: 160,
      clientY: 50,
      pointerId: 5,
      pointerType: 'touch',
    });
    expect(screen.getByTestId('video-current-time')).toHaveTextContent('0:40.000');
    fireEvent.pointerUp(surface, { pointerId: 4, pointerType: 'touch' });
    expect(seek).toHaveBeenCalledOnce();

    fireEvent.pointerDown(surface, {
      clientX: 40,
      clientY: 50,
      pointerId: 6,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(surface, {
      clientX: 140,
      clientY: 54,
      pointerId: 6,
      pointerType: 'touch',
    });
    expect(screen.getByTestId('video-current-time')).toHaveTextContent('1:40.000');
    fireEvent.lostPointerCapture(surface, { pointerId: 6, pointerType: 'touch' });
    expect(screen.getByTestId('video-current-time')).toHaveTextContent('0:40.000');
    fireEvent.pointerUp(surface, { pointerId: 6, pointerType: 'touch' });
    expect(seek).toHaveBeenCalledOnce();

    fireEvent.pointerDown(surface, {
      clientX: 40,
      clientY: 50,
      pointerId: 7,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(surface, {
      clientX: 140,
      clientY: 54,
      pointerId: 7,
      pointerType: 'touch',
    });
    fireEvent.pointerCancel(surface, { pointerId: 7, pointerType: 'touch' });
    expect(seek).toHaveBeenCalledOnce();

    act(() => {
      video.volume = 0.8;
    });
    fireEvent.pointerDown(surface, {
      clientX: 160,
      clientY: 20,
      pointerId: 2,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(surface, {
      clientX: 158,
      clientY: 70,
      pointerId: 2,
      pointerType: 'touch',
    });
    expect(video.volume).toBeCloseTo(0.3);
    fireEvent.pointerUp(surface, { clientX: 158, clientY: 70, pointerId: 2, pointerType: 'touch' });

    act(() => {
      video.volume = 0.8;
      video.muted = true;
    });
    fireEvent.pointerDown(surface, {
      clientX: 160,
      clientY: 70,
      pointerId: 3,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(surface, {
      clientX: 158,
      clientY: 20,
      pointerId: 3,
      pointerType: 'touch',
    });
    expect(video.muted).toBe(false);
    fireEvent.pointerCancel(surface, { pointerId: 3, pointerType: 'touch' });
    expect(video.volume).toBe(0.8);
    expect(video.muted).toBe(true);
  });

  it('左纵滑仅修改 video 视觉亮度，取消恢复，切源重置且提供无障碍提示', async () => {
    const view = renderPlayer();
    const video = view.container.querySelector('video')!;
    const surface = await screen.findByTestId('video-gesture-surface');
    vi.spyOn(surface, 'getBoundingClientRect').mockReturnValue({
      bottom: 100,
      height: 100,
      left: 0,
      right: 200,
      toJSON: () => ({}),
      top: 0,
      width: 200,
      x: 0,
      y: 0,
    });

    expect(screen.getByTestId('video-player-root')).toHaveAttribute(
      'data-system-brightness',
      'unsupported',
    );
    expect(screen.getByTestId('video-player-root')).toHaveAttribute(
      'data-player-visual-brightness',
      'available',
    );
    fireEvent.pointerDown(surface, {
      clientX: 40,
      clientY: 70,
      pointerId: 1,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(surface, {
      clientX: 42,
      clientY: 20,
      pointerId: 1,
      pointerType: 'touch',
    });
    expect(video.style.filter).toBe('brightness(1.5)');
    expect(screen.getByText(/播放器画面亮度 150%/)).toBeInTheDocument();
    fireEvent.pointerCancel(surface, { pointerId: 1, pointerType: 'touch' });
    expect(video.style.filter).toBe(`brightness(${DEFAULT_PLAYER_VISUAL_BRIGHTNESS})`);

    fireEvent.pointerDown(surface, {
      clientX: 40,
      clientY: 70,
      pointerId: 2,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(surface, {
      clientX: 42,
      clientY: 30,
      pointerId: 2,
      pointerType: 'touch',
    });
    fireEvent.pointerUp(surface, { clientX: 42, clientY: 30, pointerId: 2, pointerType: 'touch' });
    expect(video.style.filter).not.toBe(`brightness(${DEFAULT_PLAYER_VISUAL_BRIGHTNESS})`);

    view.rerender(
      <MantineProvider>
        <VideoPlayer autoPlay={false} mediaTitle="下一条" streamType="mp4" url="/video-b.mp4" />
      </MantineProvider>,
    );
    await waitFor(() =>
      expect(video.style.filter).toBe(`brightness(${DEFAULT_PLAYER_VISUAL_BRIGHTNESS})`),
    );
    expect(screen.getByRole('button', { name: '重置播放器画面亮度' })).toBeInTheDocument();
  });
});
