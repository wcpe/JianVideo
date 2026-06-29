import { render } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { MantineProvider } from '@mantine/core';
import VideoPlayer from './VideoPlayer';

// 走 streamType='mp4' 原生 video 分支，避免依赖 mpegts.js / hls.js 真实内核
vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => ({
  default: class {
    static isSupported() {
      return false;
    }
  },
}));

function renderPlayer(fill?: boolean) {
  return render(
    <MantineProvider>
      <VideoPlayer url="/api/play/1/stream" streamType="mp4" autoPlay fill={fill} />
    </MantineProvider>,
  );
}

describe('VideoPlayer 填充模式（FR-103）', () => {
  it('默认（无 fill）：视频容器维持固定 16:9 比例', () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    // 视频容器为 video 的父节点
    const videoBox = video.parentElement as HTMLElement;
    expect(videoBox.style.aspectRatio).toBe('16/9');
    expect(videoBox.style.flex).toBe('');
  });

  it('fill 模式：视频容器不再固定 16:9，而是 flex:1 填充剩余高度', () => {
    const { container } = renderPlayer(true);
    const video = container.querySelector('video') as HTMLVideoElement;
    const videoBox = video.parentElement as HTMLElement;
    // 填充模式去掉固定比例，改为 flex 填充
    expect(videoBox.style.aspectRatio).toBe('');
    expect(videoBox.style.flex).toBe('1 1 0%');
    expect(videoBox.style.minHeight).toBe('0px');
  });

  it('fill 模式：video 用 object-fit:contain（letterbox 黑边填充）', () => {
    const { container } = renderPlayer(true);
    const video = container.querySelector('video') as HTMLVideoElement;
    expect(video.style.objectFit).toBe('contain');
  });
});
