import { PlaybackCore } from '@jianvideo/player-core';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import VideoPlayer from './VideoPlayer';
import {
  DEFAULT_PLAYER_VISUAL_BRIGHTNESS,
  WebMediaSessionAdapter,
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

  it('横滑只在抬手时提交一次 seek，取消不提交且右纵滑实时调整媒体音量', async () => {
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
    fireEvent.pointerCancel(surface, { pointerId: 4, pointerType: 'touch' });
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
