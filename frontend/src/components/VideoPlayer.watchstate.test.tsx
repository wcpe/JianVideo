import { render, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { MantineProvider } from '@mantine/core';
import VideoPlayer from './VideoPlayer';

// 不引入 mpegts.js：streamType='mp4' 分支使用原生 video，便于驱动 timeupdate/ended 事件
vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => ({
  default: class {
    static isSupported() {
      return false;
    }
  },
}));

// 在 jsdom 中桩 video 的 currentTime/duration（默认只读/不可控）
function stubVideo(video: HTMLVideoElement, duration: number) {
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
    get: () => ({ length: 1, start: () => 0, end: () => duration }),
  });
}

function renderPlayer(props: Partial<React.ComponentProps<typeof VideoPlayer>>) {
  return render(
    <MantineProvider>
      <VideoPlayer url="/dummy.mp4" streamType="mp4" autoPlay={false} {...props} />
    </MantineProvider>,
  );
}

describe('VideoPlayer 续播与观看状态（FR-44）', () => {
  it('media 可定位后 seek 到 initialPosition', async () => {
    const { container } = renderPlayer({ initialPosition: 100 });
    const video = container.querySelector('video')!;
    stubVideo(video, 6600);

    act(() => {
      video.dispatchEvent(new Event('progress'));
      video.dispatchEvent(new Event('loadedmetadata'));
    });
    await waitFor(() => expect(video.currentTime).toBe(100));
  });

  it('initialPosition 接近片尾时不回跳', async () => {
    const { container } = renderPlayer({ initialPosition: 6595 });
    const video = container.querySelector('video')!;
    stubVideo(video, 6600); // 剩余 5s，小于阈值 15s

    await act(async () => {
      video.dispatchEvent(new Event('loadedmetadata'));
      await Promise.resolve();
    });
    expect(video.currentTime).toBe(0);
  });

  it('播放中按节流上报位置', async () => {
    const onPositionReport = vi.fn();
    const { container } = renderPlayer({ onPositionReport });
    const video = container.querySelector('video')!;
    stubVideo(video, 6600);

    // 首次 timeupdate（>=10s）上报一次
    await act(async () => {
      video.currentTime = 12;
      video.dispatchEvent(new Event('timeupdate'));
      await Promise.resolve();
    });
    expect(onPositionReport).toHaveBeenCalledWith(12);

    // 紧接着 +5s 未达节流间隔，不重复上报
    await act(async () => {
      video.currentTime = 17;
      video.dispatchEvent(new Event('timeupdate'));
      await Promise.resolve();
    });
    expect(onPositionReport).toHaveBeenCalledTimes(1);

    // 再 +10s 达到间隔，再次上报
    await act(async () => {
      video.currentTime = 23;
      video.dispatchEvent(new Event('timeupdate'));
      await Promise.resolve();
    });
    expect(onPositionReport).toHaveBeenCalledTimes(2);
    expect(onPositionReport).toHaveBeenLastCalledWith(23);
  });

  it('暂停时补报一次当前位置', async () => {
    const onPositionReport = vi.fn();
    const { container } = renderPlayer({ onPositionReport });
    const video = container.querySelector('video')!;
    stubVideo(video, 6600);

    await act(async () => {
      video.currentTime = 5;
      video.dispatchEvent(new Event('pause'));
      await Promise.resolve();
    });
    expect(onPositionReport).toHaveBeenCalledWith(5);
  });

  it('接近片尾触发 onEnded 一次', async () => {
    const onEnded = vi.fn();
    const { container } = renderPlayer({ onEnded });
    const video = container.querySelector('video')!;
    stubVideo(video, 6600);

    // 剩余 < 15s 视为看完
    await act(async () => {
      video.currentTime = 6590;
      video.dispatchEvent(new Event('timeupdate'));
      await Promise.resolve();
    });
    expect(onEnded).toHaveBeenCalledTimes(1);

    // 再次 timeupdate 不重复触发
    await act(async () => {
      video.currentTime = 6595;
      video.dispatchEvent(new Event('timeupdate'));
      await Promise.resolve();
    });
    expect(onEnded).toHaveBeenCalledTimes(1);
  });
});
