import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { PlaybackCore } from '@jianvideo/player-core';
import type {
  FrameStepResult,
  PlaybackCompletionStatus,
  WatchStateReport,
  WatchStateSendResult,
  WatchStateSnapshot,
  WatchStateTransport,
} from '@jianvideo/player-core';
import { describe, expect, it, vi } from 'vitest';
import { WebPlaybackBackend } from '@/player/WebPlaybackBackend';
import VideoPlayer from './VideoPlayer';

interface FakeHlsInstance {
  attachMedia: ReturnType<typeof vi.fn>;
  autoLevelCapping: number;
  currentLevel: number;
  destroy: ReturnType<typeof vi.fn>;
  handlers: Map<string, (...args: unknown[]) => void>;
  levels: Array<{ bitrate: number; height: number; width: number }>;
  loadSource: ReturnType<typeof vi.fn>;
  loadingEnabled: boolean;
  recoverMediaError: ReturnType<typeof vi.fn>;
  startLoad: ReturnType<typeof vi.fn>;
  stopLoad: ReturnType<typeof vi.fn>;
}

const hlsMock = vi.hoisted(() => ({ instances: [] as FakeHlsInstance[] }));

vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => {
  class FakeHls {
    static ErrorTypes = { MEDIA_ERROR: 'mediaError' };
    static Events = { ERROR: 'error', LEVEL_SWITCHED: 'level', MANIFEST_PARSED: 'manifest' };
    static isSupported() {
      return true;
    }

    attachMedia = vi.fn();
    autoLevelCapping = -1;
    currentLevel = -1;
    destroy = vi.fn();
    handlers = new Map<string, (...args: unknown[]) => void>();
    levels = [{ bitrate: 1_000_000, height: 480, width: 854 }];
    loadSource = vi.fn();
    loadingEnabled = false;
    recoverMediaError = vi.fn();
    startLoad = vi.fn(() => {
      this.loadingEnabled = true;
    });
    stopLoad = vi.fn(() => {
      this.loadingEnabled = false;
    });

    constructor() {
      hlsMock.instances.push(this);
    }

    on(event: string, handler: (...args: unknown[]) => void) {
      this.handlers.set(event, handler);
    }
  }

  return { default: FakeHls };
});

const EMPTY_WATCH_STATE: WatchStateSnapshot = {
  completed: false,
  positionSeconds: 0,
  revision: 0,
};

class FakeMediaSession {
  readonly handlers = new Map<string, (() => void) | null>();
  metadata: unknown = null;
  playbackState: MediaSessionPlaybackState = 'none';

  setActionHandler(action: MediaSessionAction, handler: (() => void) | null) {
    this.handlers.set(action, handler);
  }

  setPositionState() {}
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function stubVideo(video: HTMLVideoElement, duration: number) {
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
    paused: { configurable: true, get: () => false },
    seekable: {
      configurable: true,
      get: () => ({ length: 1, start: () => 0, end: () => duration }),
    },
  });
}

function stubControlledSeekPause(video: HTMLVideoElement, duration: number) {
  let currentTime = 0;
  let paused = false;
  let pauseOnWrite: 'microtask' | 'sync' | 'task' | null = null;
  Object.defineProperties(video, {
    currentTime: {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
        if (pauseOnWrite === null) return;
        paused = true;
        if (pauseOnWrite === 'sync') video.dispatchEvent(new Event('pause'));
        if (pauseOnWrite === 'microtask') {
          queueMicrotask(() => video.dispatchEvent(new Event('pause')));
        }
        if (pauseOnWrite === 'task') {
          setTimeout(() => video.dispatchEvent(new Event('pause')), 0);
        }
      },
    },
    duration: { configurable: true, get: () => duration },
    paused: { configurable: true, get: () => paused },
    seekable: {
      configurable: true,
      get: () => ({ length: 1, start: () => 0, end: () => duration }),
    },
  });
  return {
    enableAsyncPauseOnWrite: () => {
      pauseOnWrite = 'microtask';
    },
    enablePauseOnWrite: () => {
      pauseOnWrite = 'sync';
    },
    enableTaskPauseOnWrite: () => {
      pauseOnWrite = 'task';
    },
    setCurrentTime: (value: number) => {
      currentTime = value;
    },
  };
}

function stubReplacementPauseTimeline(video: HTMLVideoElement, duration: number) {
  let currentTime = 0;
  let pauseOnWrite = false;
  let paused = true;
  const play = vi.fn(() => {
    paused = false;
    return Promise.resolve();
  });
  Object.defineProperties(video, {
    currentTime: {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
        if (pauseOnWrite) paused = true;
      },
    },
    duration: { configurable: true, get: () => duration },
    load: { configurable: true, value: vi.fn() },
    pause: {
      configurable: true,
      value: vi.fn(() => {
        paused = true;
      }),
    },
    paused: { configurable: true, get: () => paused },
    play: { configurable: true, value: play },
    seekable: {
      configurable: true,
      get: () => ({ length: 1, start: () => 0, end: () => duration }),
    },
  });
  return {
    enablePauseOnWrite: () => {
      pauseOnWrite = true;
    },
    play,
    setPausedPosition: (position: number) => {
      currentTime = position;
      paused = true;
    },
  };
}

function stubPausedVideo(video: HTMLVideoElement, duration: number) {
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
    pause: { configurable: true, value: vi.fn() },
    paused: { configurable: true, get: () => true },
    play: { configurable: true, value: vi.fn(() => Promise.resolve()) },
    seekable: {
      configurable: true,
      get: () => ({ length: 1, start: () => 0, end: () => duration }),
    },
  });
}

function stubDelayedControlledSeekPause(video: HTMLVideoElement, duration: number) {
  let currentTime = 0;
  let paused = true;
  const pause = vi.fn(() => {
    paused = true;
  });
  const play = vi.fn(() => {
    paused = false;
    return Promise.resolve();
  });
  Object.defineProperties(video, {
    currentTime: {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
        if (!paused) paused = true;
      },
    },
    duration: { configurable: true, get: () => duration },
    pause: { configurable: true, value: pause },
    paused: { configurable: true, get: () => paused },
    play: { configurable: true, value: play },
    seekable: {
      configurable: true,
      get: () => ({ length: 1, start: () => 0, end: () => duration }),
    },
  });
  return { pause, play };
}

function stubPendingSeekRestore(video: HTMLVideoElement, duration: number) {
  let currentTime = 0;
  let paused = true;
  let playCount = 0;
  const restorePlay = deferred<void>();
  const play = vi.fn(() => {
    playCount += 1;
    if (playCount === 1) {
      paused = false;
      return Promise.resolve();
    }
    paused = false;
    return restorePlay.promise;
  });
  const pause = vi.fn(() => {
    paused = true;
    video.dispatchEvent(new Event('pause'));
  });
  Object.defineProperties(video, {
    currentTime: {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
        paused = true;
        video.dispatchEvent(new Event('pause'));
      },
    },
    duration: { configurable: true, get: () => duration },
    pause: { configurable: true, value: pause },
    paused: { configurable: true, get: () => paused },
    play: { configurable: true, value: play },
    seekable: {
      configurable: true,
      get: () => ({ length: 1, start: () => 0, end: () => duration }),
    },
  });
  return {
    play,
    resolveRestorePlay: () => restorePlay.resolve(),
    setCurrentTime: (value: number) => {
      currentTime = value;
    },
  };
}

function createTransport(initialRevision = 0): {
  readonly send: ReturnType<typeof vi.fn<WatchStateTransport['send']>>;
  readonly transport: WatchStateTransport;
} {
  let revision = initialRevision;
  const send = vi.fn<WatchStateTransport['send']>((event) => {
    revision += 1;
    return Promise.resolve({
      applied: true,
      current: {
        completed: event.eventType === 'ended',
        positionSeconds: event.eventType === 'ended' ? 0 : event.positionSeconds,
        revision,
      },
      kind: 'applied',
    } satisfies WatchStateSendResult);
  });
  return { send, transport: { send } };
}

function renderPlayer(props: Partial<React.ComponentProps<typeof VideoPlayer>> = {}) {
  return render(
    <MantineProvider>
      <VideoPlayer
        url="/dummy.mp4"
        streamType="mp4"
        autoPlay={false}
        watchContextKey="space-default:9"
        watchState={EMPTY_WATCH_STATE}
        {...props}
      />
    </MantineProvider>,
  );
}

function watchEvents(
  send: ReturnType<typeof vi.fn<WatchStateTransport['send']>>,
): WatchStateReport[] {
  return send.mock.calls.map(([event]) => event);
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

describe('VideoPlayer watch-state 完整协议', () => {
  it('媒体可定位后从 watch_states 真源恢复并上报 restore seek', async () => {
    const { send, transport } = createTransport(6);
    const { container } = renderPlayer({
      watchState: { completed: false, positionSeconds: 100, revision: 6 },
      watchStateTransport: transport,
    });
    const video = container.querySelector('video')!;
    stubVideo(video, 6600);

    act(() => {
      video.dispatchEvent(new Event('progress'));
      video.dispatchEvent(new Event('loadedmetadata'));
    });

    await waitFor(() => expect(video.currentTime).toBe(100));
    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({
          eventSeq: 1,
          eventType: 'seek',
          expectedRevision: 6,
          positionSeconds: 100,
          reason: 'restore',
        }),
      ),
    );
  });

  it('已完成或不超过 1 秒的状态不触发续播 seek', async () => {
    const completed = createTransport(3);
    const first = renderPlayer({
      watchState: { completed: true, positionSeconds: 100, revision: 3 },
      watchStateTransport: completed.transport,
    });
    const completedVideo = first.container.querySelector('video')!;
    stubVideo(completedVideo, 120);
    act(() => completedVideo.dispatchEvent(new Event('loadedmetadata')));
    await act(async () => Promise.resolve());
    expect(completedVideo.currentTime).toBe(0);
    expect(completed.send).not.toHaveBeenCalled();
    first.unmount();

    const start = createTransport(0);
    const second = renderPlayer({
      watchState: { completed: false, positionSeconds: 1, revision: 0 },
      watchStateTransport: start.transport,
    });
    const startVideo = second.container.querySelector('video')!;
    stubVideo(startVideo, 120);
    act(() => startVideo.dispatchEvent(new Event('loadedmetadata')));
    await act(async () => Promise.resolve());
    expect(startVideo.currentTime).toBe(0);
    expect(start.send).not.toHaveBeenCalled();
  });

  it('progress 合并节流，pause 与 ended 使用完整事件且不在接近片尾时自行判定完成', async () => {
    const { send, transport } = createTransport();
    const { container } = renderPlayer({ watchStateTransport: transport });
    const video = container.querySelector('video')!;
    stubVideo(video, 100);

    await act(async () => {
      video.currentTime = 12;
      video.dispatchEvent(new Event('timeupdate'));
      video.currentTime = 17;
      video.dispatchEvent(new Event('timeupdate'));
      video.currentTime = 96;
      video.dispatchEvent(new Event('timeupdate'));
      await Promise.resolve();
    });

    await waitFor(() =>
      expect(watchEvents(send).filter((event) => event.eventType === 'progress')).toHaveLength(2),
    );
    expect(watchEvents(send).filter((event) => event.eventType === 'ended')).toHaveLength(0);

    await act(async () => {
      video.dispatchEvent(new Event('pause'));
      video.dispatchEvent(new Event('ended'));
      await Promise.resolve();
    });

    await waitFor(() =>
      expect(watchEvents(send).some((event) => event.eventType === 'ended')).toBe(true),
    );
    expect(watchEvents(send)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ eventType: 'progress', positionSeconds: 12, reason: 'system' }),
        expect.objectContaining({ eventType: 'progress', positionSeconds: 96, reason: 'system' }),
        expect.objectContaining({ eventType: 'pause', positionSeconds: 96 }),
        expect.objectContaining({ eventType: 'ended', positionSeconds: 96, reason: 'system' }),
      ]),
    );
  });

  it('用户 seek 通过 core 完成事件上报 user reason', async () => {
    const { send, transport } = createTransport();
    const { container } = renderPlayer({ watchStateTransport: transport });
    const video = container.querySelector('video')!;
    stubVideo(video, 100);
    act(() => video.dispatchEvent(new Event('progress')));

    fireEvent.keyDown(container.querySelector('[data-testid="video-player-root"]')!, {
      key: 'ArrowRight',
    });

    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'seek', positionSeconds: 5, reason: 'user' }),
      ),
    );
  });

  it('seek 未触发原生 pause 时后续系统 pause 不被旧令牌吞掉', async () => {
    const { send, transport } = createTransport();
    const { container } = renderPlayer({ watchStateTransport: transport });
    const video = container.querySelector('video')!;
    stubVideo(video, 100);
    act(() => video.dispatchEvent(new Event('progress')));

    fireEvent.keyDown(container.querySelector('[data-testid="video-player-root"]')!, {
      key: 'ArrowRight',
    });
    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'seek', positionSeconds: 5, reason: 'user' }),
      ),
    );
    send.mockClear();

    act(() => video.dispatchEvent(new Event('pause')));

    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'pause', positionSeconds: 5, reason: 'system' }),
      ),
    );
  });

  it('A-B 受控回跳同步触发原生 pause 时仅上报 ab_loop seek', async () => {
    const { send, transport } = createTransport();
    const onEnded = vi.fn();
    const { container } = renderPlayer({ onEnded, watchStateTransport: transport });
    const video = container.querySelector('video')!;
    const timeline = stubControlledSeekPause(video, 100);
    await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));

    timeline.setCurrentTime(90);
    act(() => video.dispatchEvent(new Event('timeupdate')));
    await userEvent.click(screen.getByRole('button', { name: 'A-B 循环' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设置 A 点' }));
    timeline.setCurrentTime(99);
    act(() => video.dispatchEvent(new Event('timeupdate')));
    await userEvent.click(screen.getByRole('button', { name: 'A-B 循环' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设置 B 点' }));
    send.mockClear();
    timeline.enablePauseOnWrite();

    act(() => video.dispatchEvent(new Event('timeupdate')));

    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'seek', positionSeconds: 90, reason: 'ab_loop' }),
      ),
    );
    expect(watchEvents(send).filter((event) => event.eventType === 'pause')).toHaveLength(0);
    expect(onEnded).not.toHaveBeenCalled();
  });

  it('A-B 受控回跳异步触发原生 pause 时仍仅上报 ab_loop seek', async () => {
    const { send, transport } = createTransport();
    const { container } = renderPlayer({ watchStateTransport: transport });
    const video = container.querySelector('video')!;
    const timeline = stubControlledSeekPause(video, 100);
    await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));

    timeline.setCurrentTime(90);
    act(() => video.dispatchEvent(new Event('timeupdate')));
    await userEvent.click(screen.getByRole('button', { name: 'A-B 循环' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设置 A 点' }));
    timeline.setCurrentTime(99);
    act(() => video.dispatchEvent(new Event('timeupdate')));
    await userEvent.click(screen.getByRole('button', { name: 'A-B 循环' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设置 B 点' }));
    send.mockClear();
    timeline.enableAsyncPauseOnWrite();

    act(() => video.dispatchEvent(new Event('timeupdate')));

    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'seek', positionSeconds: 90, reason: 'ab_loop' }),
      ),
    );
    expect(watchEvents(send).filter((event) => event.eventType === 'pause')).toHaveLength(0);
  });

  it('受控 seek 完成后 task 级迟到 pause 仍只上报 seek', async () => {
    const { send, transport } = createTransport();
    const { container } = renderPlayer({ watchStateTransport: transport });
    const video = container.querySelector('video')!;
    const timeline = stubControlledSeekPause(video, 100);
    await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));
    act(() => video.dispatchEvent(new Event('progress')));
    timeline.enableTaskPauseOnWrite();

    fireEvent.keyDown(container.querySelector('[data-testid="video-player-root"]')!, {
      key: 'ArrowRight',
    });

    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'seek', positionSeconds: 5, reason: 'user' }),
      ),
    );
    await act(async () => {
      await new Promise<void>((resolve) => setTimeout(resolve, 0));
    });

    expect(watchEvents(send).filter((event) => event.eventType === 'pause')).toHaveLength(0);
  });

  it('replacePlayback 后旧代 tombstone 不吞新实例真实 pause 上报', async () => {
    hlsMock.instances.length = 0;
    const backends: WebPlaybackBackend[] = [];
    const originalLoad = WebPlaybackBackend.prototype.load;
    const load = vi.spyOn(WebPlaybackBackend.prototype, 'load').mockImplementation(async function (
      this: WebPlaybackBackend,
      ...args
    ) {
      backends.push(this);
      return originalLoad.apply(this, args);
    });
    try {
      const { send, transport } = createTransport();
      const onPositionReport = vi.fn();
      const { container } = renderPlayer({
        autoPlay: true,
        onPositionReport,
        watchStateTransport: transport,
      });
      const video = container.querySelector('video')!;
      const timeline = stubReplacementPauseTimeline(video, 100);
      await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));
      await waitFor(() => expect(timeline.play).toHaveBeenCalledOnce());
      timeline.enablePauseOnWrite();

      fireEvent.keyDown(container.querySelector('[data-testid="video-player-root"]')!, {
        key: 'ArrowRight',
      });
      await waitFor(() =>
        expect(watchEvents(send)).toContainEqual(
          expect.objectContaining({ eventType: 'seek', positionSeconds: 5, reason: 'user' }),
        ),
      );

      const activeBackend = backends.at(-1);
      if (!activeBackend) throw new Error('未捕获播放后端');
      const snapshot = activeBackend.getSnapshot();
      if (snapshot.sourceId === null) throw new Error('播放源尚未建立');
      const replacing = activeBackend.transactAudioSource(
        '/audio-b.m3u8',
        'space-a',
        {
          requestId: snapshot.requestId,
          sourceEpoch: snapshot.sourceEpoch,
          sourceId: snapshot.sourceId,
        },
        new AbortController().signal,
      );
      await waitFor(() => expect(hlsMock.instances).toHaveLength(1));
      const hls = hlsMock.instances[0]!;
      act(() => {
        hls.handlers.get('manifest')?.();
        video.dispatchEvent(new Event('canplay'));
      });
      await act(async () => replacing);
      send.mockClear();
      onPositionReport.mockClear();

      timeline.setPausedPosition(40);
      act(() => video.dispatchEvent(new Event('pause')));

      await waitFor(() =>
        expect(watchEvents(send)).toContainEqual(
          expect.objectContaining({ eventType: 'pause', positionSeconds: 40, reason: 'system' }),
        ),
      );
      expect(onPositionReport).toHaveBeenCalledWith(40);
    } finally {
      load.mockRestore();
    }
  });

  it.each(['unsupported', 'failed'] as const)(
    '独立逐帧返回 %s 且无原生 pause 时后续系统 pause 保持 system',
    async (status) => {
      const stepFrame = vi
        .spyOn(PlaybackCore.prototype, 'stepFrame')
        .mockResolvedValue(frameStepResult(status));
      try {
        const { send, transport } = createTransport();
        const { container } = renderPlayer({ watchStateTransport: transport });
        const video = container.querySelector('video')!;
        stubVideo(video, 100);
        await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));

        fireEvent.click(screen.getByRole('button', { name: '后一帧' }));
        await waitFor(() => expect(stepFrame).toHaveBeenCalledOnce());
        await act(async () => Promise.resolve());
        send.mockClear();
        video.currentTime = 20;
        act(() => video.dispatchEvent(new Event('pause')));

        await waitFor(() =>
          expect(watchEvents(send)).toContainEqual(
            expect.objectContaining({ eventType: 'pause', positionSeconds: 20, reason: 'system' }),
          ),
        );
      } finally {
        stepFrame.mockRestore();
      }
    },
  );

  it.each(['unsupported', 'failed'] as const)(
    '1 帧定位档返回 %s 且无原生 pause 时后续系统 pause 保持 system',
    async (status) => {
      const seekByTier = vi
        .spyOn(PlaybackCore.prototype, 'seekByTier')
        .mockResolvedValue({ requestId: 2, status });
      try {
        const { send, transport } = createTransport();
        const { container } = renderPlayer({ watchStateTransport: transport });
        const video = container.querySelector('video')!;
        stubVideo(video, 100);
        await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));
        fireEvent.click(screen.getByRole('button', { name: '定位档位：5 秒' }));
        fireEvent.click(await screen.findByRole('menuitem', { name: '1 帧' }));
        await screen.findByRole('button', { name: '定位档位：1 帧' });

        fireEvent.click(screen.getByRole('button', { name: '前进 1 帧' }));
        await waitFor(() =>
          expect(seekByTier).toHaveBeenCalledTimes(status === 'unsupported' ? 2 : 1),
        );
        await act(async () => Promise.resolve());
        send.mockClear();
        video.currentTime = 20;
        act(() => video.dispatchEvent(new Event('pause')));

        await waitFor(() =>
          expect(watchEvents(send)).toContainEqual(
            expect.objectContaining({ eventType: 'pause', positionSeconds: 20, reason: 'system' }),
          ),
        );
      } finally {
        seekByTier.mockRestore();
      }
    },
  );

  it.each(['pause', 'stop', 'frame-step', 'frame-tier'] as const)(
    '已暂停态成功 %s 且无原生 pause 时后续系统 pause 保持 system',
    async (action) => {
      const session = new FakeMediaSession();
      Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
      const command =
        action === 'pause' || action === 'stop'
          ? vi
              .spyOn(PlaybackCore.prototype, action)
              .mockResolvedValue({ requestId: 2, status: 'completed' })
          : action === 'frame-step'
            ? vi
                .spyOn(PlaybackCore.prototype, 'stepFrame')
                .mockResolvedValue(frameStepResult('completed'))
            : vi
                .spyOn(PlaybackCore.prototype, 'seekByTier')
                .mockResolvedValue({ requestId: 2, status: 'completed' });
      try {
        const { send, transport } = createTransport();
        const { container } = renderPlayer({ watchStateTransport: transport });
        const video = container.querySelector('video')!;
        stubPausedVideo(video, 100);
        await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));

        if (action === 'pause' || action === 'stop') {
          await waitFor(() => expect(session.handlers.get(action)).toEqual(expect.any(Function)));
          act(() => session.handlers.get(action)?.());
        } else if (action === 'frame-step') {
          fireEvent.click(screen.getByRole('button', { name: '后一帧' }));
        } else {
          fireEvent.click(screen.getByRole('button', { name: '定位档位：5 秒' }));
          fireEvent.click(await screen.findByRole('menuitem', { name: '1 帧' }));
          fireEvent.click(await screen.findByRole('button', { name: '前进 1 帧' }));
        }
        await waitFor(() => expect(command).toHaveBeenCalledOnce());
        await act(async () => Promise.resolve());
        send.mockClear();

        video.currentTime = 20;
        act(() => video.dispatchEvent(new Event('pause')));

        await waitFor(() =>
          expect(watchEvents(send)).toContainEqual(
            expect.objectContaining({ eventType: 'pause', positionSeconds: 20, reason: 'system' }),
          ),
        );
      } finally {
        command.mockRestore();
        Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: undefined });
      }
    },
  );

  it.each(['play', 'pause', 'stop'] as const)(
    'seek 已建立 tombstone 后 manual %s 不得让迟到 pause 上报',
    async (action) => {
      const session = new FakeMediaSession();
      Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
      const stop =
        action === 'stop'
          ? vi
              .spyOn(PlaybackCore.prototype, 'stop')
              .mockResolvedValue({ requestId: 4, status: 'completed' })
          : null;
      try {
        const { send, transport } = createTransport();
        const { container } = renderPlayer({ autoPlay: true, watchStateTransport: transport });
        const video = container.querySelector('video')!;
        const timeline = stubDelayedControlledSeekPause(video, 100);
        act(() => video.dispatchEvent(new Event('progress')));
        await waitFor(() => expect(timeline.play).toHaveBeenCalledOnce());
        await waitFor(() => expect(session.handlers.get(action)).toEqual(expect.any(Function)));

        fireEvent.keyDown(container.querySelector('[data-testid="video-player-root"]')!, {
          key: 'ArrowRight',
        });
        await waitFor(() => expect(timeline.play).toHaveBeenCalledTimes(2));
        await waitFor(() =>
          expect(watchEvents(send)).toContainEqual(
            expect.objectContaining({ eventType: 'seek', positionSeconds: 5, reason: 'user' }),
          ),
        );
        await act(async () => {
          await new Promise<void>((resolve) => setTimeout(resolve, 0));
          await new Promise<void>((resolve) => setTimeout(resolve, 0));
        });
        send.mockClear();

        act(() => session.handlers.get(action)?.());
        if (action === 'stop') await waitFor(() => expect(stop).toHaveBeenCalledOnce());
        if (action === 'play') await waitFor(() => expect(timeline.play).toHaveBeenCalledTimes(3));
        if (action === 'pause') await waitFor(() => expect(timeline.pause).toHaveBeenCalledOnce());
        act(() => video.dispatchEvent(new Event('pause')));
        await act(async () => Promise.resolve());

        expect(watchEvents(send).filter((event) => event.eventType === 'pause')).toHaveLength(0);
      } finally {
        stop?.mockRestore();
        Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: undefined });
      }
    },
  );

  it.each([
    ['pause', 'unsupported'],
    ['pause', 'failed'],
    ['pause', 'superseded'],
    ['stop', 'unsupported'],
    ['stop', 'failed'],
    ['stop', 'superseded'],
  ] as const)(
    'Media Session %s 返回 %s 且无原生 pause 时后续系统 pause 保持 system',
    async (action, status) => {
      const session = new FakeMediaSession();
      const command = vi
        .spyOn(PlaybackCore.prototype, action)
        .mockResolvedValue({ requestId: 2, status });
      Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
      try {
        const { send, transport } = createTransport();
        const { container } = renderPlayer({ watchStateTransport: transport });
        const video = container.querySelector('video')!;
        stubVideo(video, 100);
        await waitFor(() => expect(session.handlers.get(action)).toEqual(expect.any(Function)));

        act(() => session.handlers.get(action)?.());
        await waitFor(() => expect(command).toHaveBeenCalledOnce());
        await act(async () => Promise.resolve());
        send.mockClear();
        video.currentTime = 20;
        act(() => video.dispatchEvent(new Event('pause')));

        await waitFor(() =>
          expect(watchEvents(send)).toContainEqual(
            expect.objectContaining({ eventType: 'pause', positionSeconds: 20, reason: 'system' }),
          ),
        );
      } finally {
        command.mockRestore();
        Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: undefined });
      }
    },
  );

  it.each(['pause', 'stop'] as const)(
    'seek 恢复播放 pending 时用户 %s pause 上报 user，后续系统 pause 不继承 user',
    async (action) => {
      const session = new FakeMediaSession();
      Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: session });
      try {
        const { send, transport } = createTransport();
        const { container } = renderPlayer({ autoPlay: true, watchStateTransport: transport });
        const video = container.querySelector('video')!;
        const timeline = stubPendingSeekRestore(video, 100);
        act(() => video.dispatchEvent(new Event('progress')));
        await waitFor(() => expect(timeline.play).toHaveBeenCalledOnce());
        await waitFor(() => expect(session.handlers.get(action)).toEqual(expect.any(Function)));
        send.mockClear();

        fireEvent.keyDown(container.querySelector('[data-testid="video-player-root"]')!, {
          key: 'ArrowRight',
        });
        await waitFor(() => expect(timeline.play).toHaveBeenCalledTimes(2));
        act(() => session.handlers.get(action)?.());

        await waitFor(() =>
          expect(watchEvents(send)).toContainEqual(
            expect.objectContaining({ eventType: 'pause', positionSeconds: 5, reason: 'user' }),
          ),
        );

        timeline.resolveRestorePlay();
        await act(async () => {
          await Promise.resolve();
          await Promise.resolve();
        });
        timeline.setCurrentTime(30);
        act(() => video.dispatchEvent(new Event('pause')));

        await waitFor(() =>
          expect(watchEvents(send)).toContainEqual(
            expect.objectContaining({ eventType: 'pause', positionSeconds: 30, reason: 'system' }),
          ),
        );
        expect(watchEvents(send)).not.toContainEqual(
          expect.objectContaining({ eventType: 'pause', positionSeconds: 30, reason: 'user' }),
        );
      } finally {
        Object.defineProperty(navigator, 'mediaSession', { configurable: true, value: undefined });
      }
    },
  );

  it('暂停态逐帧成功按确认时间上报 user seek，失败和旧源完成不报', async () => {
    let coreListener: Parameters<PlaybackCore['subscribe']>[0] | undefined;
    let getActiveSnapshot: PlaybackCore['getSnapshot'] | undefined;
    const originalSubscribe = PlaybackCore.prototype.subscribe;
    const subscribe = vi.spyOn(PlaybackCore.prototype, 'subscribe').mockImplementation(function (
      this: PlaybackCore,
      listener,
    ) {
      coreListener = listener;
      getActiveSnapshot = this.getSnapshot.bind(this);
      return originalSubscribe.call(this, listener);
    });
    const { send, transport } = createTransport();
    const { container } = renderPlayer({ watchStateTransport: transport });
    const video = container.querySelector('video')!;
    stubVideo(video, 100);
    await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));
    act(() => video.dispatchEvent(new Event('pause')));
    await waitFor(() => expect(send).toHaveBeenCalled());
    send.mockClear();
    const snapshot = getActiveSnapshot!();
    const result = {
      clamped: false,
      confirmedMediaTime: 4.04,
      correctionCount: 0,
      direction: 'next' as const,
      frameDuration: 0.04,
      precision: 'approximate' as const,
      requestId: snapshot.requestId,
      startMediaTime: 4,
      status: 'completed' as const,
      targetMediaTime: 4.04,
      timestampError: 0,
    };

    act(() =>
      coreListener?.({
        requestId: result.requestId,
        result,
        sourceEpoch: snapshot.sourceEpoch,
        sourceId: snapshot.sourceId,
        type: 'frameStepCompleted',
      }),
    );
    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'seek', positionSeconds: 4.04, reason: 'user' }),
      ),
    );
    const reportCount = send.mock.calls.length;

    act(() => {
      coreListener?.({
        requestId: result.requestId,
        result: {
          ...result,
          error: { category: 'media', message: '逐帧失败' },
          status: 'failed',
        },
        sourceEpoch: snapshot.sourceEpoch,
        sourceId: snapshot.sourceId,
        type: 'frameStepCompleted',
      });
      coreListener?.({
        requestId: result.requestId,
        result,
        sourceEpoch: snapshot.sourceEpoch + 1,
        sourceId: snapshot.sourceId,
        type: 'frameStepCompleted',
      });
    });
    await act(async () => Promise.resolve());
    expect(send).toHaveBeenCalledTimes(reportCount);
    subscribe.mockRestore();
  });

  it('用户按钮暂停上报 user reason，原生系统暂停保持 system reason', async () => {
    const { send, transport } = createTransport();
    const { container } = renderPlayer({ watchStateTransport: transport });
    const video = container.querySelector('video')!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));
    stubVideo(video, 120);
    Object.defineProperties(video, {
      pause: { configurable: true, value: vi.fn() },
      play: { configurable: true, value: vi.fn(() => Promise.resolve()) },
    });
    video.currentTime = 24;

    fireEvent.click(screen.getByRole('button', { name: '播放' }));
    fireEvent.click(await screen.findByRole('button', { name: '暂停' }));
    act(() => video.dispatchEvent(new Event('pause')));

    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'pause', positionSeconds: 24, reason: 'user' }),
      ),
    );

    video.currentTime = 30;
    act(() => video.dispatchEvent(new Event('pause')));
    await waitFor(() =>
      expect(watchEvents(send)).toContainEqual(
        expect.objectContaining({ eventType: 'pause', positionSeconds: 30, reason: 'system' }),
      ),
    );
  });

  it('不触发 pagehide 的组件卸载仍以最新位置关闭 reporter', () => {
    const { send, transport } = createTransport();
    const view = renderPlayer({ watchStateTransport: transport });
    const video = view.container.querySelector('video')!;
    stubVideo(video, 120);
    video.currentTime = 47;

    view.unmount();

    const closingCall = send.mock.calls.at(-1);
    expect(closingCall?.[0]).toMatchObject({
      eventType: 'pause',
      positionSeconds: 47,
      reason: 'system',
    });
    expect(closingCall?.[1]).toEqual({ keepalive: true });
  });

  it('visibility hidden 补报 keepalive，pagehide 关闭当前 session 且不再接收新事件', async () => {
    const { send, transport } = createTransport();
    const { container } = renderPlayer({ watchStateTransport: transport });
    const video = container.querySelector('video')!;
    stubVideo(video, 120);
    video.currentTime = 30;

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    act(() => document.dispatchEvent(new Event('visibilitychange')));
    await waitFor(() => expect(send).toHaveBeenCalled());
    expect(send.mock.calls[0]![0]).toMatchObject({
      eventType: 'pause',
      positionSeconds: 30,
      reason: 'system',
    });
    expect(send.mock.calls[0]![1]).toEqual({ keepalive: true });

    act(() => window.dispatchEvent(new Event('pagehide')));
    await act(async () => Promise.resolve());
    const countAfterPageHide = send.mock.calls.length;
    act(() => {
      video.currentTime = 50;
      video.dispatchEvent(new Event('timeupdate'));
    });
    await act(async () => Promise.resolve());
    expect(send).toHaveBeenCalledTimes(countAfterPageHide);
  });
});
