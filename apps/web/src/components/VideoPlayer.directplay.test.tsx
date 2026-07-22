import { render, screen, act, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
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

// jsdom 不实现 HTMLMediaElement.play；打桩为成功 resolve，并据 muted 标志驱动 playing 状态。
// setup 未把 HTMLMediaElement 挂到全局，故从 video 元素实例的原型链取得其原型。
function stubVideoPlay() {
  const proto = Object.getPrototypeOf(
    Object.getPrototypeOf(document.createElement('video')),
  ) as HTMLMediaElement;
  // 模拟浏览器：muted autoplay 被允许，play() 成功 resolve
  const playSpy = vi.fn(() => Promise.resolve());
  Object.defineProperty(proto, 'play', {
    configurable: true,
    writable: true,
    value: playSpy,
  });
  return playSpy;
}

function renderPlayer() {
  return render(
    <MantineProvider>
      <VideoPlayer url="/api/play/1/stream" streamType="mp4" autoPlay />
    </MantineProvider>,
  );
}

describe('VideoPlayer 直接播放（FR-102）', () => {
  beforeEach(() => stubVideoPlay());

  it('autoplay 时先静音再播放，进页即播放且不显示「点击播放」遮罩', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    await waitFor(() => expect(video.muted).toBe(true));
    // 未被自动播放策略拦截：无「点击播放」兜底遮罩
    expect(screen.queryByText('点击播放')).toBeNull();
  });

  it('静音自动播后展示「点击取消静音」入口，点击后恢复有声（muted=false）', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video') as HTMLVideoElement;
    const btn = await screen.findByRole('button', { name: '点击取消静音' });
    expect(video.muted).toBe(true);
    await act(async () => {
      await userEvent.click(btn);
    });
    expect(video.muted).toBe(false);
    // 恢复有声后该入口消失
    expect(screen.queryByRole('button', { name: '点击取消静音' })).toBeNull();
  });
});
