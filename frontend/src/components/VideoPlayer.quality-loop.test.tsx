import { PlaybackCore } from '@jianvideo/player-core';
import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import VideoPlayer from './VideoPlayer';
import { WebPlaybackBackend } from '@/player/WebPlaybackBackend';

interface FakeHlsInstance {
  autoLevelCapping: number;
  currentLevel: number;
  handlers: Map<string, (...args: unknown[]) => void>;
  levels: Array<{ bitrate: number; height: number; width: number }>;
  loadingEnabled: boolean;
  startLoad: ReturnType<typeof vi.fn>;
  stopLoad: ReturnType<typeof vi.fn>;
}

const hlsMock = vi.hoisted(() => ({
  instances: [] as FakeHlsInstance[],
  levels: [
    { bitrate: 5_000_000, height: 1080, width: 1920 },
    { bitrate: 2_500_000, height: 720, width: 1280 },
    { bitrate: 1_000_000, height: 480, width: 854 },
    { bitrate: 500_000, height: 360, width: 640 },
  ],
}));

vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => {
  class FakeHls {
    static Events = { ERROR: 'error', LEVEL_SWITCHED: 'level', MANIFEST_PARSED: 'manifest' };
    static isSupported() {
      return true;
    }

    autoLevelCapping = -1;
    currentLevel = -1;
    handlers = new Map<string, (...args: unknown[]) => void>();
    levels = hlsMock.levels.map((level) => ({ ...level }));
    loadingEnabled = false;
    startLoad = vi.fn(() => {
      this.loadingEnabled = true;
    });
    stopLoad = vi.fn(() => {
      this.loadingEnabled = false;
    });

    constructor() {
      hlsMock.instances.push(this);
    }

    attachMedia() {}
    destroy() {}
    loadSource() {}
    on(event: string, handler: (...args: unknown[]) => void) {
      this.handlers.set(event, handler);
    }
  }
  return { default: FakeHls };
});

function stubMediaMethods(): void {
  const proto = Object.getPrototypeOf(
    Object.getPrototypeOf(document.createElement('video')),
  ) as HTMLMediaElement;
  Object.defineProperty(proto, 'play', {
    configurable: true,
    writable: true,
    value: vi.fn(() => Promise.resolve()),
  });
  Object.defineProperty(proto, 'pause', { configurable: true, writable: true, value: vi.fn() });
  Object.defineProperty(proto, 'load', { configurable: true, writable: true, value: vi.fn() });
}

function stubTimeline(video: HTMLVideoElement, duration = 60): void {
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
    seekable: {
      configurable: true,
      get: () => ({ length: 1, start: () => 0, end: () => duration }),
    },
  });
}

function renderPlayer(url = '/master.m3u8') {
  return render(
    <MantineProvider>
      <VideoPlayer url={url} isABR autoPlay={false} />
    </MantineProvider>,
  );
}

async function readyHls(): Promise<FakeHlsInstance> {
  await act(async () => {
    await vi.dynamicImportSettled();
  });
  const hls = hlsMock.instances.at(-1)!;
  act(() => hls.handlers.get('manifest')?.());
  await waitFor(() => expect(screen.getByRole('button', { name: '清晰度' })).toBeInTheDocument());
  return hls;
}

beforeEach(() => {
  hlsMock.instances.length = 0;
  hlsMock.levels = [
    { bitrate: 5_000_000, height: 1080, width: 1920 },
    { bitrate: 2_500_000, height: 720, width: 1280 },
    { bitrate: 1_000_000, height: 480, width: 854 },
    { bitrate: 500_000, height: 360, width: 640 },
  ];
  stubMediaMethods();
  vi.stubGlobal('MediaSource', { isTypeSupported: () => true });
});

afterEach(async () => {
  cleanup();
  await vi.dynamicImportSettled();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('VideoPlayer 清晰度与省流量', () => {
  it('展示自动实际档位，手动锁档并可恢复自动', async () => {
    renderPlayer();
    const hls = await readyHls();
    act(() => hls.handlers.get('level')?.('level', { level: 2 }));
    expect(await screen.findByText('自动（当前 480p）')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: '清晰度' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '720p' }));
    expect(hls.currentLevel).toBe(1);
    expect(await screen.findByText('720p（手动）')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: '清晰度' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '自动' }));
    await waitFor(() => expect(hls.currentLevel).toBe(-1));
  });

  it('省流量限制 480p，手动选择 720p 时先关闭省流量', async () => {
    renderPlayer();
    const hls = await readyHls();

    await userEvent.click(screen.getByRole('switch', { name: '省流量' }));
    await waitFor(() => expect(hls.autoLevelCapping).toBe(2));
    expect(screen.getByRole('switch', { name: '省流量' })).toBeChecked();

    await userEvent.click(screen.getByRole('button', { name: '清晰度' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '720p' }));
    expect(hls.autoLevelCapping).toBe(-1);
    expect(hls.currentLevel).toBe(1);
    expect(screen.getByRole('switch', { name: '省流量' })).not.toBeChecked();
  });

  it('无 480p 或更低档位时阻断播放与加载，关闭省流量后解除', async () => {
    hlsMock.levels = hlsMock.levels.slice(0, 2);
    renderPlayer();
    const hls = await readyHls();

    await userEvent.click(screen.getByRole('switch', { name: '省流量' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('当前视频无 480p 或更低档位');
    expect(hls.stopLoad).toHaveBeenCalled();
    expect(screen.getByRole('button', { name: '播放' })).toBeDisabled();

    await userEvent.click(screen.getByRole('switch', { name: '省流量' }));
    await waitFor(() => expect(screen.queryByText(/当前视频无 480p/)).not.toBeInTheDocument());
    expect(screen.getByRole('button', { name: '播放' })).not.toBeDisabled();
  });
});

describe('VideoPlayer 倍速与 A-B 循环', () => {
  it('七档倍速统一调用 PlaybackCore.setPlaybackRate，切换媒体重置 1x', async () => {
    const setPlaybackRate = vi.spyOn(PlaybackCore.prototype, 'setPlaybackRate');
    const view = renderPlayer();
    await readyHls();
    const video = view.container.querySelector('video')!;

    await userEvent.click(screen.getByRole('button', { name: '播放速度' }));
    for (const label of ['0.5×', '0.75×', '1×', '1.25×', '1.5×', '1.75×', '2×']) {
      expect(screen.getByRole('menuitem', { name: label })).toBeInTheDocument();
    }
    await userEvent.click(screen.getByRole('menuitem', { name: '1.75×' }));
    expect(setPlaybackRate).toHaveBeenCalledWith(1.75);
    expect(video.playbackRate).toBe(1.75);

    view.rerender(
      <MantineProvider>
        <VideoPlayer url="/next-master.m3u8" isABR autoPlay={false} />
      </MantineProvider>,
    );
    await act(async () => {
      await vi.dynamicImportSettled();
    });
    act(() => hlsMock.instances.at(-1)?.handlers.get('manifest')?.());
    await waitFor(() => expect(video.playbackRate).toBe(1));
  });

  it('A-B 校验最小区间，仅到达 B 后以 ab_loop 回跳，清除和切源重置', async () => {
    const seek = vi.spyOn(WebPlaybackBackend.prototype, 'seek');
    const view = renderPlayer();
    await readyHls();
    const video = view.container.querySelector('video')!;
    stubTimeline(video);

    act(() => {
      video.currentTime = 10;
      video.dispatchEvent(new Event('timeupdate'));
    });
    await userEvent.click(screen.getByRole('button', { name: 'A-B 循环' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设置 A 点' }));
    act(() => {
      video.currentTime = 10.4;
      video.dispatchEvent(new Event('timeupdate'));
    });
    await userEvent.click(screen.getByRole('button', { name: 'A-B 循环' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设置 B 点' }));
    expect(screen.getByText(/A 0:10.000/)).toHaveTextContent('B 未设置');

    act(() => {
      video.currentTime = 12;
      video.dispatchEvent(new Event('timeupdate'));
    });
    await userEvent.click(screen.getByRole('button', { name: 'A-B 循环' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '设置 B 点' }));
    expect(screen.getByText(/A 0:10.000/)).toHaveTextContent('B 0:12.000');

    act(() => {
      video.currentTime = 11.999;
      video.dispatchEvent(new Event('timeupdate'));
    });
    expect(seek.mock.calls.some(([request]) => request.reason === 'ab_loop')).toBe(false);

    act(() => {
      video.currentTime = 12;
      video.dispatchEvent(new Event('timeupdate'));
    });
    await waitFor(() => expect(seek).toHaveBeenCalledWith(expect.objectContaining({ reason: 'ab_loop', targetTime: 10 })));

    const onEnded = vi.fn();
    view.rerender(
      <MantineProvider>
        <VideoPlayer url="/master.m3u8" isABR autoPlay={false} onEnded={onEnded} />
      </MantineProvider>,
    );
    act(() => {
      video.currentTime = 12;
      video.dispatchEvent(new Event('ended'));
    });
    expect(onEnded).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: 'A-B 循环' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '清除 A-B' }));
    expect(screen.getByText('A/B 未设置')).toBeInTheDocument();

    view.rerender(
      <MantineProvider>
        <VideoPlayer url="/other-master.m3u8" isABR autoPlay={false} />
      </MantineProvider>,
    );
    await act(async () => {
      await vi.dynamicImportSettled();
    });
    act(() => hlsMock.instances.at(-1)?.handlers.get('manifest')?.());
    expect(await screen.findByText('A/B 未设置')).toBeInTheDocument();
  });
});
