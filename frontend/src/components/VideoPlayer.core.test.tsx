import { PlaybackCore } from '@jianvideo/player-core';
import type { SeekRequest, SeekResult } from '@jianvideo/player-core';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { WebFramePresentationFacet } from '@/player/WebFramePresentationFacet';
import { WebPlaybackBackend } from '@/player/WebPlaybackBackend';
import VideoPlayer from './VideoPlayer';

interface FakeMpegtsPlayer {
  handlers: Map<string, (...args: unknown[]) => void>;
  load: ReturnType<typeof vi.fn>;
  pause: ReturnType<typeof vi.fn>;
  play: ReturnType<typeof vi.fn>;
}

interface FakeHlsInstance {
  handlers: Map<string, (...args: unknown[]) => void>;
  loadSource: ReturnType<typeof vi.fn>;
}

const mpegtsMock = vi.hoisted(() => ({
  instances: [] as FakeMpegtsPlayer[],
}));

const hlsMock = vi.hoisted(() => ({
  instances: [] as FakeHlsInstance[],
}));

vi.mock('@mantine/core', async () => {
  const actual = await vi.importActual<typeof import('@mantine/core')>('@mantine/core');
  interface SliderProps {
    'aria-label'?: string;
    max?: number;
    min?: number;
    onChange?(value: number): void;
    step?: number;
    value: number;
  }
  const Slider = ({
    'aria-label': ariaLabel,
    max = 100,
    min = 0,
    onChange,
    step = 1,
    value,
  }: SliderProps) => (
    <input
      aria-label={ariaLabel}
      max={max}
      min={min}
      onChange={(event) => onChange?.(Number(event.currentTarget.value))}
      step={step}
      type="range"
      value={value}
    />
  );
  return { ...actual, Slider };
});

vi.mock('mpegts.js', () => ({
  default: {
    createPlayer: () => {
      const handlers = new Map<string, (...args: unknown[]) => void>();
      const player = {
        attachMediaElement: vi.fn(),
        destroy: vi.fn(),
        handlers,
        load: vi.fn(),
        on: vi.fn((event: string, handler: (...args: unknown[]) => void) => {
          handlers.set(event, handler);
        }),
        pause: vi.fn(),
        play: vi.fn(() => Promise.resolve()),
        unload: vi.fn(),
      };
      mpegtsMock.instances.push(player);
      return player;
    },
  },
}));

vi.mock('hls.js', () => {
  class FakeHls {
    static Events = { ERROR: 'error', LEVEL_SWITCHED: 'level', MANIFEST_PARSED: 'manifest' };
    static isSupported() {
      return true;
    }
    attachMedia = vi.fn();
    destroy = vi.fn();
    handlers = new Map<string, (...args: unknown[]) => void>();
    levels = [{ height: 720, width: 1280 }];
    loadSource = vi.fn();

    constructor() {
      hlsMock.instances.push(this);
    }

    on(event: string, handler: (...args: unknown[]) => void) {
      this.handlers.set(event, handler);
    }
  }

  return { default: FakeHls };
});

function renderPlayer(props?: Partial<React.ComponentProps<typeof VideoPlayer>>) {
  return render(
    <MantineProvider>
      <VideoPlayer url="/video.mp4" streamType="mp4" autoPlay={false} {...props} />
    </MantineProvider>,
  );
}

function timeRanges(ranges: Array<[number, number]>): TimeRanges {
  return {
    length: ranges.length,
    start: (index: number) => ranges[index][0],
    end: (index: number) => ranges[index][1],
  };
}

function stubTimeline(
  video: HTMLVideoElement,
  duration: number,
  initialRanges: Array<[number, number]>,
) {
  let currentTime = 0;
  let ranges = initialRanges;
  Object.defineProperties(video, {
    currentTime: {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    },
    duration: { configurable: true, get: () => duration },
    seekable: { configurable: true, get: () => timeRanges(ranges) },
  });
  return {
    setCurrentTime: (value: number) => {
      currentTime = value;
    },
    setRanges: (next: Array<[number, number]>) => {
      ranges = next;
    },
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

async function waitForHlsInstance() {
  await vi.dynamicImportSettled();
  await waitFor(() => expect(hlsMock.instances.length).toBeGreaterThan(0));
  return hlsMock.instances.at(-1)!;
}

function stubMediaMethods() {
  const prototype = Object.getPrototypeOf(
    Object.getPrototypeOf(document.createElement('video')),
  ) as HTMLMediaElement;
  Object.defineProperty(prototype, 'play', {
    configurable: true,
    value: vi.fn(() => Promise.resolve()),
    writable: true,
  });
  Object.defineProperty(prototype, 'pause', {
    configurable: true,
    value: vi.fn(),
    writable: true,
  });
}

beforeEach(() => {
  vi.stubGlobal('MediaSource', { isTypeSupported: () => true });
  mpegtsMock.instances.length = 0;
  hlsMock.instances.length = 0;
  stubMediaMethods();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('VideoPlayer 真实 PlaybackCore→WebPlaybackBackend 接入', () => {
  it('URL/描述符变化保持 native、mpegts、HLS 三内核选择', async () => {
    const view = renderPlayer();
    const video = view.container.querySelector('video')!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video.mp4'));

    view.rerender(
      <MantineProvider>
        <VideoPlayer url="/stream.ts" streamType="mpegts" autoPlay={false} />
      </MantineProvider>,
    );
    await waitFor(() => expect(mpegtsMock.instances).toHaveLength(1));

    view.rerender(
      <MantineProvider>
        <VideoPlayer url="/master.m3u8" isABR autoPlay={false} />
      </MantineProvider>,
    );
    const hls = await waitForHlsInstance();
    expect(hls.loadSource).toHaveBeenCalledWith('/master.m3u8');
  });

  it('二进制帧 marker 由播放器接成真实像素身份 resolver', async () => {
    const load = vi.spyOn(WebFramePresentationFacet.prototype, 'load');
    renderPlayer({
      frameMarker: { bits: 9, cellSize: 8, x: 16, y: 16 },
      frameTimeline: [
        { mediaTime: 0, sourceFrameIndex: 0, stableFrameId: 'binary-marker:0' },
        { mediaTime: 1 / 30, sourceFrameIndex: 1, stableFrameId: 'binary-marker:1' },
      ],
      nominalFrameRate: 30,
    });

    await waitFor(() => expect(load).toHaveBeenCalled());
    expect(load.mock.calls.at(-1)?.[0]).toMatchObject({
      nominalFrameRate: 30,
      resolvePresentedFrameIdentity: expect.any(Function),
    });
  });

  it('mpegts autoplay 只在 core.load 等到 loadeddata 完成后发起', async () => {
    renderPlayer({ autoPlay: true, streamType: 'mpegts', url: '/stream.ts' });
    await waitFor(() => expect(mpegtsMock.instances).toHaveLength(1));
    const player = mpegtsMock.instances[0];

    expect(player.play).not.toHaveBeenCalled();
    act(() => player.handlers.get('loadeddata')?.());
    await waitFor(() => expect(player.play).toHaveBeenCalledOnce());
  });

  it('HLS autoplay 只在清单与 canplay 均就绪后发起', async () => {
    const { container } = renderPlayer({
      autoPlay: true,
      isABR: true,
      streamType: 'mpegts',
      url: '/master.m3u8',
    });
    const video = container.querySelector('video')!;
    const hls = await waitForHlsInstance();

    expect(video.play).not.toHaveBeenCalled();
    act(() => {
      hls.handlers.get('manifest')?.();
      video.dispatchEvent(new Event('canplay'));
    });
    await waitFor(() => expect(video.play).toHaveBeenCalledOnce());
  });

  it('播放、暂停、进度条与当前 5 秒档统一经过真实 core/backend', async () => {
    const user = userEvent.setup();
    const { container } = renderPlayer();
    const video = container.querySelector('video')!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video.mp4'));
    const timeline = stubTimeline(video, 100, [[0, 100]]);
    timeline.setCurrentTime(20);
    act(() => video.dispatchEvent(new Event('progress')));
    act(() => video.dispatchEvent(new Event('timeupdate')));

    await user.click(screen.getByRole('button', { name: '播放' }));
    await user.click(await screen.findByRole('button', { name: '暂停' }));
    await user.click(screen.getByRole('button', { name: '前进 5 秒' }));
    await waitFor(() => expect(video.currentTime).toBe(25));
    await user.click(screen.getByRole('button', { name: '后退 5 秒' }));
    await waitFor(() => expect(video.currentTime).toBe(20));

    fireEvent.change(screen.getByRole('slider', { name: '播放进度' }), {
      target: { value: '21' },
    });
    await waitFor(() => expect(video.currentTime).toBe(21));
    expect(video.play).toHaveBeenCalledOnce();
    expect(video.pause).toHaveBeenCalledOnce();
  });
});

describe('VideoPlayer 真实 core restore Seek', () => {
  it('initialPosition 通过 core.seek(restore)，并发媒体事件只发一次且夹取成功后完成', async () => {
    const seekSpy = vi.spyOn(WebPlaybackBackend.prototype, 'seek');
    const { container } = renderPlayer({ initialPosition: 100 });
    const video = container.querySelector('video')!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video.mp4'));
    stubTimeline(video, 1000, [[0, 80]]);
    act(() => video.dispatchEvent(new Event('progress')));

    act(() => {
      video.dispatchEvent(new Event('loadedmetadata'));
      video.dispatchEvent(new Event('durationchange'));
    });

    await waitFor(() => expect(video.currentTime).toBe(80));
    expect(seekSpy).toHaveBeenCalledOnce();
    expect(seekSpy.mock.calls[0][0]).toMatchObject({ reason: 'restore', requestedTime: 100 });
    act(() => video.dispatchEvent(new Event('durationchange')));
    expect(seekSpy).toHaveBeenCalledOnce();
  });

  it('metadata 时 restore unsupported 后，仅 progress 令 seekable 可用也会自动重试', async () => {
    const coreSeekSpy = vi.spyOn(PlaybackCore.prototype, 'seek');
    const backendSeekSpy = vi.spyOn(WebPlaybackBackend.prototype, 'seek');
    const { container } = renderPlayer({ initialPosition: 100 });
    const video = container.querySelector('video')!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video.mp4'));
    const timeline = stubTimeline(video, 1000, []);

    act(() => {
      video.dispatchEvent(new Event('loadedmetadata'));
      video.dispatchEvent(new Event('durationchange'));
    });
    await waitFor(() => expect(coreSeekSpy).toHaveBeenCalledOnce());
    expect(coreSeekSpy).toHaveBeenCalledWith(100, 'restore');
    expect(backendSeekSpy).not.toHaveBeenCalled();

    timeline.setRanges([[0, 1000]]);
    act(() => video.dispatchEvent(new Event('progress')));

    await waitFor(() => expect(video.currentTime).toBe(100));
    expect(coreSeekSpy).toHaveBeenCalledTimes(2);
    expect(coreSeekSpy).toHaveBeenLastCalledWith(100, 'restore');
    expect(backendSeekSpy).toHaveBeenCalledOnce();
  });

  it('failed restore 不置完成标志，后续媒体事件仍会重试', async () => {
    const originalSeek = WebPlaybackBackend.prototype.seek;
    const seekSpy = vi
      .spyOn(WebPlaybackBackend.prototype, 'seek')
      .mockImplementationOnce(async function (this: WebPlaybackBackend, request: SeekRequest) {
        const result = await originalSeek.call(this, request);
        return {
          ...result,
          error: { category: 'media', message: '定位失败' },
          status: 'failed',
        };
      })
      .mockImplementation(function (this: WebPlaybackBackend, request: SeekRequest) {
        return originalSeek.call(this, request);
      });
    const { container } = renderPlayer({ initialPosition: 100 });
    const video = container.querySelector('video')!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/video.mp4'));
    const timeline = stubTimeline(video, 1000, [[0, 1000]]);
    act(() => video.dispatchEvent(new Event('progress')));
    act(() => video.dispatchEvent(new Event('loadedmetadata')));
    await waitFor(() => expect(seekSpy).toHaveBeenCalledOnce());

    timeline.setCurrentTime(0);
    act(() => video.dispatchEvent(new Event('progress')));
    act(() => video.dispatchEvent(new Event('durationchange')));
    await waitFor(() => expect(seekSpy).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(video.currentTime).toBe(100));
  });

  it('切源后旧 restore 结果不得污染新源的一次性状态', async () => {
    const originalSeek = WebPlaybackBackend.prototype.seek;
    const oldSeek = deferred<SeekResult>();
    const seekSpy = vi
      .spyOn(WebPlaybackBackend.prototype, 'seek')
      .mockImplementationOnce((_request: SeekRequest) => oldSeek.promise)
      .mockImplementation(function (this: WebPlaybackBackend, request: SeekRequest) {
        return originalSeek.call(this, request);
      });
    const view = renderPlayer({ initialPosition: 100, url: '/old.mp4' });
    const video = view.container.querySelector('video')!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/old.mp4'));
    stubTimeline(video, 1000, [[0, 1000]]);
    act(() => video.dispatchEvent(new Event('progress')));
    act(() => video.dispatchEvent(new Event('loadedmetadata')));
    await waitFor(() => expect(seekSpy).toHaveBeenCalledOnce());

    view.rerender(
      <MantineProvider>
        <VideoPlayer url="/new.mp4" streamType="mp4" autoPlay={false} initialPosition={200} />
      </MantineProvider>,
    );
    await waitFor(() => expect(video.getAttribute('src')).toBe('/new.mp4'));
    await waitFor(() => expect(seekSpy).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(video.currentTime).toBe(200));

    oldSeek.resolve({
      clamped: false,
      confirmedTime: 100,
      requestId: 2,
      requestedTime: 100,
      status: 'completed',
      targetTime: 100,
    });
    await act(async () => Promise.resolve());
    act(() => video.dispatchEvent(new Event('durationchange')));

    expect(video.currentTime).toBe(200);
    expect(seekSpy).toHaveBeenCalledTimes(2);
  });
});
