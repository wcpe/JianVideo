import type { PreparedPreviewTrack } from '@jianvideo/player-core';
import { PlaybackCore, PreparedPreviewFacet } from '@jianvideo/player-core';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import VideoPlayer from './VideoPlayer';

vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => ({
  default: class {
    static isSupported() {
      return false;
    }
  },
}));

interface PendingImage {
  readonly src: string;
  fail(): void;
  load(): void;
}

const pendingImages: PendingImage[] = [];

class MockImage {
  onerror: (() => void) | null = null;
  onload: (() => void) | null = null;
  private value = '';

  set src(value: string) {
    this.value = value;
    pendingImages.push({
      fail: () => this.onerror?.(),
      load: () => this.onload?.(),
      src: value,
    });
  }

  get src(): string {
    return this.value;
  }
}

const previewTrack: PreparedPreviewTrack = {
  cues: [
    {
      endTime: 5,
      sprite: { assetId: 'sheet-a', height: 45, width: 80, x: 10, y: 20 },
      startTime: 0,
    },
    {
      endTime: 10,
      sprite: { assetId: 'sheet-b', height: 54, width: 96, x: 30, y: 40 },
      startTime: 5,
    },
  ],
  generationId: 'generation-1',
  mediaId: 'media-1',
  profileId: 'profile-1',
  sourceFingerprint: 'fingerprint-1',
};

const gappedPreviewTrack: PreparedPreviewTrack = {
  ...previewTrack,
  cues: [previewTrack.cues[0]!, { ...previewTrack.cues[1]!, startTime: 7 }],
};

function stubMedia() {
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

function stubTimeline(video: HTMLVideoElement, duration = 10) {
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
      get: () => ({ end: () => duration, length: 1, start: () => 0 }),
    },
  });
}

function renderPreviewPlayer(withTrack = true, track: PreparedPreviewTrack = previewTrack) {
  const previewProps = withTrack
    ? {
        previewSpriteUrls: { 'sheet-a': '/sheet-a.jpg', 'sheet-b': '/sheet-b.jpg' },
        previewTrack: track,
      }
    : {};
  const view = render(
    <MantineProvider>
      <VideoPlayer autoPlay={false} streamType="mp4" url="/preview.mp4" {...previewProps} />
    </MantineProvider>,
  );
  const video = view.container.querySelector('video')!;
  stubTimeline(video);
  const progress = screen.getByTestId('video-progress-preview');
  vi.spyOn(progress, 'getBoundingClientRect').mockReturnValue({
    bottom: 16,
    height: 16,
    left: 0,
    right: 100,
    toJSON: () => ({}),
    top: 0,
    width: 100,
    x: 0,
    y: 0,
  });
  act(() => {
    video.dispatchEvent(new Event('durationchange'));
    video.dispatchEvent(new Event('progress'));
  });
  return { ...view, progress, video };
}

beforeEach(() => {
  pendingImages.length = 0;
  stubMedia();
  vi.stubGlobal('Image', MockImage);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('VideoPlayer 时间轴预览（FR2-029）', () => {
  it('hover 通过轨道命中并在图片加载后显示时间与精灵裁剪', async () => {
    const { progress, video } = renderPreviewPlayer();
    await waitFor(() => expect(video.getAttribute('src')).toBe('/preview.mp4'));

    fireEvent.mouseMove(progress, { clientX: 25 });

    expect(screen.getByText('0:02')).toBeInTheDocument();
    expect(pendingImages).toHaveLength(1);
    act(() => pendingImages[0].load());
    const sprite = await screen.findByTestId('timeline-preview-sprite');
    const image = sprite.querySelector('img')!;
    expect(image).toHaveAttribute('src', '/sheet-a.jpg');
    expect(sprite).toHaveStyle({ height: '45px', overflow: 'hidden', width: '80px' });
    expect(image.style.transform).toBe('translate(-10px, -20px)');
  });

  it('图片 Promise 按 URL 复用，快速移动时旧加载结果不覆盖新命中', async () => {
    const { progress, video } = renderPreviewPlayer();
    await waitFor(() => expect(video.getAttribute('src')).toBe('/preview.mp4'));
    fireEvent.mouseMove(progress, { clientX: 20 });
    fireEvent.mouseMove(progress, { clientX: 30 });
    await waitFor(() => {
      expect(pendingImages.map((image) => image.src)).toEqual(['/sheet-a.jpg']);
    });

    fireEvent.mouseMove(progress, { clientX: 80 });
    await waitFor(() => {
      expect(pendingImages.map((image) => image.src)).toEqual(['/sheet-a.jpg', '/sheet-b.jpg']);
    });
    act(() => pendingImages[0].load());
    expect(screen.queryByTestId('timeline-preview-sprite')).not.toBeInTheDocument();

    act(() => pendingImages[1].load());
    const image = (await screen.findByTestId('timeline-preview-sprite')).querySelector('img')!;
    expect(image).toHaveAttribute('src', '/sheet-b.jpg');
  });

  it('鼠标离开后隐藏预览并忽略迟到图片', async () => {
    const { progress, video } = renderPreviewPlayer();
    await waitFor(() => expect(video.getAttribute('src')).toBe('/preview.mp4'));
    fireEvent.mouseMove(progress, { clientX: 25 });
    expect(await screen.findByText('0:02')).toBeInTheDocument();

    fireEvent.mouseLeave(progress);
    act(() => pendingImages[0].load());

    expect(screen.queryByText('0:02')).not.toBeInTheDocument();
    expect(screen.queryByTestId('timeline-preview-sprite')).not.toBeInTheDocument();
  });

  it('轨道清空后按当前源上下文清除命中', async () => {
    const view = renderPreviewPlayer();
    await waitFor(() => expect(view.video.getAttribute('src')).toBe('/preview.mp4'));
    fireEvent.mouseMove(view.progress, { clientX: 25 });
    expect(await screen.findByText('0:02')).toBeInTheDocument();
    act(() => pendingImages[0].load());
    expect(await screen.findByTestId('timeline-preview-sprite')).toBeInTheDocument();

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay={false}
          previewSpriteUrls={{ 'sheet-a': '/sheet-a.jpg' }}
          streamType="mp4"
          url="/preview.mp4"
        />
      </MantineProvider>,
    );
    fireEvent.mouseMove(view.progress, { clientX: 25 });

    await waitFor(() => {
      expect(screen.queryByTestId('timeline-preview-sprite')).not.toBeInTheDocument();
    });
  });

  it('浮层按精灵半宽夹紧边缘，无图时至少保留 5%', async () => {
    const view = renderPreviewPlayer();
    await waitFor(() => expect(view.video.getAttribute('src')).toBe('/preview.mp4'));

    fireEvent.mouseMove(view.progress, { clientX: 0 });
    act(() => pendingImages[0].load());
    expect(await screen.findByTestId('timeline-preview-overlay')).toHaveStyle({ left: '40%' });

    fireEvent.mouseMove(view.progress, { clientX: 99 });
    act(() => pendingImages[1].load());
    expect(await screen.findByTestId('timeline-preview-overlay')).toHaveStyle({ left: '52%' });

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay={false}
          previewTrack={previewTrack}
          streamType="mp4"
          url="/preview.mp4"
        />
      </MantineProvider>,
    );
    fireEvent.mouseMove(view.progress, { clientX: 0 });
    expect(await screen.findByTestId('timeline-preview-overlay')).toHaveStyle({ left: '5%' });
  });

  it('图片元素失败时清除当前图片并保留时间', async () => {
    const { progress, video } = renderPreviewPlayer();
    await waitFor(() => expect(video.getAttribute('src')).toBe('/preview.mp4'));
    fireEvent.mouseMove(progress, { clientX: 25 });
    act(() => pendingImages[0].load());
    const sprite = await screen.findByTestId('timeline-preview-sprite');

    fireEvent.error(sprite.querySelector('img')!);

    expect(screen.queryByTestId('timeline-preview-sprite')).not.toBeInTheDocument();
    expect(screen.getByText('0:02')).toBeInTheDocument();
  });

  it('切换播放源时先清空旧轨道再绑定当前轨道', async () => {
    const setTrack = vi.spyOn(PreparedPreviewFacet.prototype, 'setTrack');
    const view = renderPreviewPlayer();
    await waitFor(() => expect(view.video.getAttribute('src')).toBe('/preview.mp4'));
    setTrack.mockClear();

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay={false}
          previewSpriteUrls={{ 'sheet-a': '/sheet-a.jpg', 'sheet-b': '/sheet-b.jpg' }}
          previewTrack={previewTrack}
          streamType="mp4"
          url="/next.mp4"
        />
      </MantineProvider>,
    );
    await waitFor(() => expect(view.video.getAttribute('src')).toBe('/next.mp4'));

    expect(setTrack.mock.calls[0]?.[0]).toBeNull();
    expect(setTrack.mock.calls.some(([track]) => track === previewTrack)).toBe(true);
  });

  it('移动端长按 400ms 后可横移预览，抬起只执行一次 core.seek', async () => {
    vi.useFakeTimers();
    const seek = vi.spyOn(PlaybackCore.prototype, 'seek');
    const { progress } = renderPreviewPlayer();
    expect(progress).toHaveStyle({ touchAction: 'auto' });

    fireEvent.pointerDown(progress, {
      clientX: 20,
      clientY: 8,
      pointerId: 1,
      pointerType: 'touch',
    });
    await act(async () => vi.advanceTimersByTimeAsync(400));
    expect(progress).toHaveStyle({ touchAction: 'none' });
    expect(screen.getByText('0:02')).toBeInTheDocument();

    fireEvent.pointerMove(progress, {
      clientX: 70,
      clientY: 9,
      pointerId: 1,
      pointerType: 'touch',
    });
    expect(screen.getByText('0:07')).toBeInTheDocument();
    fireEvent.pointerUp(progress, { clientX: 70, clientY: 9, pointerId: 1, pointerType: 'touch' });

    expect(seek).toHaveBeenCalledTimes(1);
    expect(seek).toHaveBeenCalledWith(7, 'user');
    expect(screen.queryByText('0:07')).not.toBeInTheDocument();
  });

  it('无预览轨道时长按仍显示时间、横移更新并在抬起定位', async () => {
    vi.useFakeTimers();
    const seek = vi.spyOn(PlaybackCore.prototype, 'seek');
    const { progress } = renderPreviewPlayer(false);

    fireEvent.pointerDown(progress, {
      clientX: 20,
      clientY: 8,
      pointerId: 1,
      pointerType: 'touch',
    });
    await act(async () => vi.advanceTimersByTimeAsync(400));

    expect(progress).toHaveStyle({ touchAction: 'none' });
    expect(screen.getByTestId('timeline-preview-overlay')).toHaveTextContent('0:02');
    expect(screen.queryByTestId('timeline-preview-sprite')).not.toBeInTheDocument();

    fireEvent.pointerMove(progress, {
      clientX: 80,
      clientY: 9,
      pointerId: 1,
      pointerType: 'touch',
    });
    expect(screen.getByTestId('timeline-preview-overlay')).toHaveTextContent('0:08');
    fireEvent.pointerUp(progress, { clientX: 80, clientY: 9, pointerId: 1, pointerType: 'touch' });

    expect(seek).toHaveBeenCalledTimes(1);
    expect(seek).toHaveBeenCalledWith(8, 'user');
  });

  it('预览轨道命中缝隙时长按仍显示目标时间占位', async () => {
    vi.useFakeTimers();
    const { progress } = renderPreviewPlayer(true, gappedPreviewTrack);

    fireEvent.pointerDown(progress, {
      clientX: 60,
      clientY: 8,
      pointerId: 1,
      pointerType: 'touch',
    });
    await act(async () => vi.advanceTimersByTimeAsync(400));

    expect(progress).toHaveStyle({ touchAction: 'none' });
    expect(screen.getByTestId('timeline-preview-overlay')).toHaveTextContent('0:06');
    expect(screen.queryByTestId('timeline-preview-sprite')).not.toBeInTheDocument();
  });

  it('无预览轨道时桌面 hover 显示目标时间占位并在离开后隐藏', async () => {
    const { progress, video } = renderPreviewPlayer(false);
    await waitFor(() => expect(video.getAttribute('src')).toBe('/preview.mp4'));

    fireEvent.mouseMove(progress, { clientX: 60 });
    expect(screen.getByTestId('timeline-preview-overlay')).toHaveTextContent('0:06');
    expect(screen.queryByTestId('timeline-preview-sprite')).not.toBeInTheDocument();

    fireEvent.mouseLeave(progress);
    expect(screen.queryByTestId('timeline-preview-overlay')).not.toBeInTheDocument();
  });

  it('触摸 pointer 会话期间抑制底层 Slider onChange，桌面 Slider 保持可用', async () => {
    const seek = vi.spyOn(PlaybackCore.prototype, 'seek');
    const { video } = renderPreviewPlayer();
    const slider = screen.getAllByRole('slider')[1]!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/preview.mp4'));
    await waitFor(() => expect(screen.getByText('0:10')).toBeInTheDocument());

    fireEvent.pointerDown(slider, { clientX: 20, clientY: 8, pointerId: 1, pointerType: 'touch' });
    fireEvent.keyDown(slider, { key: 'ArrowRight', code: 'ArrowRight' });
    fireEvent.pointerUp(slider, { clientX: 20, clientY: 8, pointerId: 1, pointerType: 'touch' });
    expect(seek).not.toHaveBeenCalled();

    fireEvent.keyDown(slider, { key: 'ArrowRight', code: 'ArrowRight' });
    expect(seek).toHaveBeenCalledTimes(1);
  });

  it('短按、纵向移动和 pointercancel 均不触发 seek', async () => {
    vi.useFakeTimers();
    const seek = vi.spyOn(PlaybackCore.prototype, 'seek');
    const { progress } = renderPreviewPlayer();

    fireEvent.pointerDown(progress, {
      clientX: 20,
      clientY: 8,
      pointerId: 1,
      pointerType: 'touch',
    });
    fireEvent.pointerUp(progress, { clientX: 20, clientY: 8, pointerId: 1, pointerType: 'touch' });

    fireEvent.pointerDown(progress, {
      clientX: 20,
      clientY: 8,
      pointerId: 2,
      pointerType: 'touch',
    });
    fireEvent.pointerMove(progress, {
      clientX: 22,
      clientY: 30,
      pointerId: 2,
      pointerType: 'touch',
    });
    await act(async () => vi.advanceTimersByTimeAsync(400));
    fireEvent.pointerUp(progress, { clientX: 22, clientY: 30, pointerId: 2, pointerType: 'touch' });

    fireEvent.pointerDown(progress, {
      clientX: 20,
      clientY: 8,
      pointerId: 3,
      pointerType: 'touch',
    });
    await act(async () => vi.advanceTimersByTimeAsync(400));
    fireEvent.pointerCancel(progress, { pointerId: 3, pointerType: 'touch' });

    fireEvent.pointerDown(progress, {
      clientX: 20,
      clientY: 8,
      pointerId: 4,
      pointerType: 'touch',
    });
    await act(async () => vi.advanceTimersByTimeAsync(400));
    fireEvent.pointerMove(progress, {
      clientX: 25,
      clientY: 30,
      pointerId: 4,
      pointerType: 'touch',
    });
    fireEvent.pointerUp(progress, { clientX: 25, clientY: 30, pointerId: 4, pointerType: 'touch' });

    expect(seek).not.toHaveBeenCalled();
  });

  it('键盘定位保持原行为，预览层不拦截普通交互', () => {
    const seekByTier = vi.spyOn(PlaybackCore.prototype, 'seekByTier');
    const { progress } = renderPreviewPlayer();
    const root = screen.getByTestId('video-player-root');
    root.focus();

    fireEvent.mouseDown(progress, { clientX: 40 });
    fireEvent.keyDown(root, { key: 'ArrowRight' });

    expect(seekByTier).toHaveBeenCalledWith('next');
  });
});
