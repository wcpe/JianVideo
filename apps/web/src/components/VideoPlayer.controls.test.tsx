import { PlaybackCore } from '@jianvideo/player-core';
import { render, screen, act, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import VideoPlayer from './VideoPlayer';
import {
  loadVolumePref,
  saveVolumePref,
  clampVolume,
  VOLUME_PREF_KEY,
} from './VideoPlayer.helpers';

// 走 streamType='mp4' 原生 video 分支，避免依赖 mpegts.js / hls.js 真实内核
vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => ({
  default: class {
    static isSupported() {
      return false;
    }
  },
}));

// jsdom 不实现 HTMLMediaElement.play；打桩为成功 resolve
function stubVideoPlay() {
  const proto = Object.getPrototypeOf(
    Object.getPrototypeOf(document.createElement('video')),
  ) as HTMLMediaElement;
  Object.defineProperty(proto, 'play', {
    configurable: true,
    writable: true,
    value: vi.fn(() => Promise.resolve()),
  });
  Object.defineProperty(proto, 'pause', { configurable: true, writable: true, value: vi.fn() });
}

// 在 jsdom 中桩 video 的 currentTime/duration（默认只读/不可控）
function stubVideoTime(video: HTMLVideoElement, duration: number) {
  let cur = 0;
  Object.defineProperty(video, 'currentTime', {
    configurable: true,
    get: () => cur,
    set: (v: number) => {
      cur = v;
    },
  });
  Object.defineProperty(video, 'duration', { configurable: true, get: () => duration });
  Object.defineProperty(video, 'seekable', {
    configurable: true,
    get: () => ({
      length: 1,
      start: () => 0,
      end: () => duration,
    }),
  });
}

function renderPlayer(props?: Partial<React.ComponentProps<typeof VideoPlayer>>) {
  return render(
    <MantineProvider>
      <VideoPlayer url="/api/play/1/stream" streamType="mp4" autoPlay={false} {...props} />
    </MantineProvider>,
  );
}

describe('记忆音量纯函数（FR-104）', () => {
  beforeEach(() => localStorage.clear());

  it('无存储时返回 null', () => {
    expect(loadVolumePref()).toBeNull();
  });

  it('saveVolumePref 写入后 loadVolumePref 读回相同偏好', () => {
    saveVolumePref({ volume: 0.4, muted: false });
    expect(loadVolumePref()).toEqual({ volume: 0.4, muted: false });
    expect(localStorage.getItem(VOLUME_PREF_KEY)).toContain('0.4');
  });

  it('记忆静音态', () => {
    saveVolumePref({ volume: 0.8, muted: true });
    expect(loadVolumePref()).toEqual({ volume: 0.8, muted: true });
  });

  it('损坏的存储内容返回 null（不抛）', () => {
    localStorage.setItem(VOLUME_PREF_KEY, '{不是合法json');
    expect(loadVolumePref()).toBeNull();
  });

  it('越界 volume 被夹取到 [0,1]', () => {
    saveVolumePref({ volume: 5, muted: false });
    expect(loadVolumePref()?.volume).toBe(1);
  });

  it('clampVolume 把越界 / 非有限值夹取到 [0,1]', () => {
    expect(clampVolume(50)).toBe(1);
    expect(clampVolume(0.5)).toBe(0.5);
    expect(clampVolume(-0.2)).toBe(0);
    expect(clampVolume(Number.NaN)).toBe(0);
  });
});

describe('音量滑块域（FR-104 修复 IndexSizeError）', () => {
  beforeEach(() => {
    localStorage.clear();
    stubVideoPlay();
  });

  it('音量滑块域为 [0,1]，避免 onChange 输出 0-100 致 video.volume 越界崩溃', () => {
    renderPlayer();
    // 修复前音量 Slider 用 Mantine 默认 max=100，拖动时 onChange 给 0-100、handleVolume 直接赋给 v.volume 触发 IndexSizeError；
    // 修复后音量 Slider min=0 max=1，故必有一个滑块 aria-valuemax 为 '1'
    const sliders = screen.getAllByRole('slider');
    expect(sliders.some((s) => s.getAttribute('aria-valuemax') === '1')).toBe(true);
  });
});

describe('VideoPlayer 当前时间精度', () => {
  beforeEach(() => {
    stubVideoPlay();
    localStorage.clear();
  });

  it('暂停态显示三位毫秒，避免逐帧结果重复显示为 0:00', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/api/play/1/stream'));
    stubVideoTime(video, 10);

    act(() => {
      video.currentTime = 0.033;
      video.dispatchEvent(new Event('durationchange'));
      video.dispatchEvent(new Event('timeupdate'));
    });

    expect(screen.getByTestId('video-current-time')).toHaveTextContent('0:00.033');
  });

  it('播放态保持整秒显示，避免毫秒数字持续抖动', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/api/play/1/stream'));
    stubVideoTime(video, 10);

    act(() => {
      video.currentTime = 5.987;
      video.dispatchEvent(new Event('durationchange'));
      video.dispatchEvent(new Event('playing'));
      video.dispatchEvent(new Event('timeupdate'));
    });

    expect(screen.getByTestId('video-current-time')).toHaveTextContent('0:05');
  });
});

describe('VideoPlayer 倍速（FR-104）', () => {
  beforeEach(() => {
    stubVideoPlay();
    localStorage.clear();
  });

  it('选择倍速后置 video.playbackRate', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;

    await act(async () => {
      await userEvent.click(screen.getByRole('button', { name: '播放速度' }));
    });
    await act(async () => {
      await userEvent.click(screen.getByRole('menuitem', { name: '1.5×' }));
    });

    expect(video.playbackRate).toBe(1.5);
  });
});

describe('VideoPlayer 六档定位（FR2-034）', () => {
  beforeEach(() => {
    stubVideoPlay();
    localStorage.clear();
  });

  it('默认 5 秒档双向按钮通过 core.seekByTier 定位', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/api/play/1/stream'));
    stubVideoTime(video, 600);
    act(() => {
      video.currentTime = 100;
      video.dispatchEvent(new Event('timeupdate'));
    });

    await userEvent.click(screen.getByRole('button', { name: '后退 5 秒' }));
    await waitFor(() => expect(video.currentTime).toBe(95));
    await userEvent.click(screen.getByRole('button', { name: '前进 5 秒' }));
    await waitFor(() => expect(video.currentTime).toBe(100));
  });

  it('菜单包含六档，切档只调用 setSeekTier 且不位移', async () => {
    const setTier = vi.spyOn(PlaybackCore.prototype, 'setSeekTier');
    const seekByTier = vi.spyOn(PlaybackCore.prototype, 'seekByTier');
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/api/play/1/stream'));
    stubVideoTime(video, 600);
    video.currentTime = 100;

    await userEvent.click(screen.getByRole('button', { name: '定位档位：5 秒' }));
    for (const label of ['1 帧', '0.5 秒', '1 秒', '5 秒', '30 秒', '60 秒']) {
      expect(screen.getByRole('menuitem', { name: label })).toBeInTheDocument();
    }
    await userEvent.click(screen.getByRole('menuitem', { name: '30 秒' }));

    expect(setTier).toHaveBeenLastCalledWith({ kind: 'seconds', value: 30 });
    expect(seekByTier).not.toHaveBeenCalled();
    expect(video.currentTime).toBe(100);
    expect(screen.getByRole('button', { name: '后退 30 秒' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '前进 30 秒' })).toBeInTheDocument();
  });

  it('独立帧按钮调用双向 core.stepFrame', async () => {
    const stepFrame = vi.spyOn(PlaybackCore.prototype, 'stepFrame');
    renderPlayer();

    await userEvent.click(screen.getByRole('button', { name: '前一帧' }));
    await userEvent.click(screen.getByRole('button', { name: '后一帧' }));

    expect(stepFrame).toHaveBeenNthCalledWith(1, 'previous');
    expect(stepFrame).toHaveBeenNthCalledWith(2, 'next');
  });

  it('近似能力首次执行前即持续显示，逐帧失败后仍显示', async () => {
    vi.spyOn(PlaybackCore.prototype, 'stepFrame').mockResolvedValueOnce({
      clamped: false,
      confirmedMediaTime: 0,
      correctionCount: 0,
      direction: 'next',
      error: { category: 'media', message: '定位失败' },
      frameDuration: 1 / 30,
      precision: 'approximate',
      requestId: 2,
      startMediaTime: 0,
      status: 'failed',
      targetMediaTime: 1 / 30,
      timestampError: 1 / 30,
    });
    renderPlayer({ url: '/approximate.mp4' });

    expect(await screen.findByText('近似逐帧')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '后一帧' }));
    expect(screen.getByText('近似逐帧')).toBeInTheDocument();
  });

  it('exact 能力下近似结果持续提示，仅后续 exact-verified 成功清除', async () => {
    let coreListener: Parameters<PlaybackCore['subscribe']>[0] | undefined;
    let getActiveSnapshot: PlaybackCore['getSnapshot'] | undefined;
    const originalSubscribe = PlaybackCore.prototype.subscribe;
    vi.spyOn(PlaybackCore.prototype, 'subscribe').mockImplementation(function (
      this: PlaybackCore,
      listener,
    ) {
      getActiveSnapshot = this.getSnapshot.bind(this);
      coreListener = listener;
      return originalSubscribe.call(this, listener);
    });
    let frameCallback: VideoFrameRequestCallback | undefined;
    const view = renderPlayer({
      frameTimeline: [
        { mediaTime: 0, sourceFrameIndex: 0 },
        { mediaTime: 1 / 30, sourceFrameIndex: 1 },
      ],
      resolvePresentedFrameIdentity: () => ({ sourceFrameIndex: 0 }),
      url: '/runtime-approximate.mp4',
    });
    const video = view.container.querySelector('video') as HTMLVideoElement;
    Object.assign(video, {
      cancelVideoFrameCallback: vi.fn(),
      requestVideoFrameCallback: vi.fn((callback: VideoFrameRequestCallback) => {
        frameCallback = callback;
        return 1;
      }),
    });
    await waitFor(() => expect(video.getAttribute('src')).toBe('/runtime-approximate.mp4'));
    act(() => frameCallback?.(performance.now(), { mediaTime: 0 } as VideoFrameCallbackMetadata));
    await waitFor(() => expect(screen.queryByText('近似逐帧')).not.toBeInTheDocument());
    const snapshot = getActiveSnapshot!();
    const result = {
      clamped: false,
      confirmedMediaTime: 0,
      correctionCount: 0,
      direction: 'next' as const,
      frameDuration: 1 / 30,
      precision: 'approximate' as const,
      requestId: snapshot.requestId,
      startMediaTime: 0,
      status: 'completed' as const,
      targetMediaTime: 1 / 30,
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
    expect(screen.getByText('近似逐帧')).toBeInTheDocument();
    act(() =>
      coreListener?.({
        requestId: result.requestId,
        result: { ...result, precision: 'unsupported', status: 'canceled' },
        sourceEpoch: snapshot.sourceEpoch,
        sourceId: snapshot.sourceId,
        type: 'frameStepCompleted',
      }),
    );
    expect(screen.getByText('近似逐帧')).toBeInTheDocument();
    act(() =>
      coreListener?.({
        requestId: result.requestId,
        result: { ...result, precision: 'exact-verified' },
        sourceEpoch: snapshot.sourceEpoch,
        sourceId: snapshot.sourceId,
        type: 'frameStepCompleted',
      }),
    );
    expect(screen.queryByText('近似逐帧')).not.toBeInTheDocument();
  });

  it('切源时按当前 core 能力在 approximate、exact、unavailable 间同步提示', async () => {
    let frameCallback: VideoFrameRequestCallback | undefined;
    const requestVideoFrameCallback = vi.fn((callback: VideoFrameRequestCallback) => {
      frameCallback = callback;
      return 1;
    });
    const cancelVideoFrameCallback = vi.fn();
    const view = renderPlayer({ url: '/approximate.mp4' });
    const video = view.container.querySelector('video') as HTMLVideoElement;
    Object.assign(video, { cancelVideoFrameCallback, requestVideoFrameCallback });
    expect(await screen.findByText('近似逐帧')).toBeInTheDocument();

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          url="/exact.mp4"
          streamType="mp4"
          autoPlay={false}
          frameTimeline={[
            { mediaTime: 0, sourceFrameIndex: 0 },
            { mediaTime: 1 / 30, sourceFrameIndex: 1 },
          ]}
          resolvePresentedFrameIdentity={() => ({ sourceFrameIndex: 0 })}
        />
      </MantineProvider>,
    );
    await waitFor(() => {
      expect(video.getAttribute('src')).toBe('/exact.mp4');
      expect(screen.getByTestId('video-player-root')).toHaveAttribute(
        'data-frame-presentation',
        'approximate',
      );
    });
    expect(screen.getByText('近似逐帧')).toBeInTheDocument();
    act(() => {
      frameCallback?.(performance.now(), { mediaTime: 0 } as VideoFrameCallbackMetadata);
    });
    await waitFor(() => expect(screen.queryByText('近似逐帧')).not.toBeInTheDocument());

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          url="/unsupported.mp4"
          descriptor={{ codec: 'unknown', path: 'fmp4', url: '/unsupported.mp4' }}
          autoPlay={false}
        />
      </MantineProvider>,
    );
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    expect(screen.queryByText('近似逐帧')).not.toBeInTheDocument();
  });
});

describe('VideoPlayer 键盘快捷键（FR-104）', () => {
  beforeEach(() => {
    stubVideoPlay();
    localStorage.clear();
  });

  it('空格切换播放/暂停', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    const root = screen.getByTestId('video-player-root');
    root.focus();

    await act(async () => {
      await userEvent.keyboard(' ');
    });
    expect(video.play).toHaveBeenCalled();
  });

  it('M 键切换静音', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    const root = screen.getByTestId('video-player-root');
    root.focus();

    expect(video.muted).toBe(false);
    await act(async () => {
      await userEvent.keyboard('m');
    });
    expect(video.muted).toBe(true);
  });

  it('箭头按当前 5 秒档双向定位，逗号句号双向逐帧', async () => {
    const stepFrame = vi.spyOn(PlaybackCore.prototype, 'stepFrame');
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/api/play/1/stream'));
    stubVideoTime(video, 600);
    act(() => {
      video.currentTime = 100;
      video.dispatchEvent(new Event('timeupdate'));
    });
    const root = screen.getByTestId('video-player-root');
    root.focus();

    await userEvent.keyboard('{ArrowRight}');
    await waitFor(() => expect(video.currentTime).toBe(105));
    await userEvent.keyboard('{ArrowLeft}');
    await waitFor(() => expect(video.currentTime).toBe(100));
    await userEvent.keyboard(',.');

    expect(stepFrame).toHaveBeenNthCalledWith(1, 'previous');
    expect(stepFrame).toHaveBeenNthCalledWith(2, 'next');
  });

  it('焦点在输入框时不触发快捷键', async () => {
    const { container } = render(
      <MantineProvider>
        <div>
          <input data-testid="text-input" />
          <VideoPlayer url="/api/play/1/stream" streamType="mp4" autoPlay={false} />
        </div>
      </MantineProvider>,
    );
    const video = container.querySelector('video') as HTMLVideoElement;
    const input = screen.getByTestId('text-input') as HTMLInputElement;
    input.focus();

    await act(async () => {
      await userEvent.keyboard(' ');
    });
    // 焦点在输入框，空格不应触发播放
    expect(video.play).not.toHaveBeenCalled();
  });
});

describe('VideoPlayer 进度条品牌紫（FR-104 修偏 FR-93）', () => {
  beforeEach(() => {
    stubVideoPlay();
    localStorage.clear();
  });

  it('播放进度条使用品牌紫（不再用蓝）', () => {
    const { container } = renderPlayer();
    // Mantine Slider 以 --slider-color CSS 变量承载 color；品牌紫命名色为 purple
    const purpleSlider = container.querySelector('[style*="--slider-color: purple"]');
    expect(purpleSlider).not.toBeNull();
    // 不应再出现蓝色 slider（既有 color="blue" 已收敛为 purple）
    const blueSlider = container.querySelector('[style*="--slider-color: blue"]');
    expect(blueSlider).toBeNull();
  });
});
