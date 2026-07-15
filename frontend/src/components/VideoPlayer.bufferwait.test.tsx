import { render, screen, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { MantineProvider } from '@mantine/core';
import VideoPlayer from './VideoPlayer';

// 不引入 mpegts.js：streamType='mp4' 分支使用原生 video，便于驱动 waiting/canplay 事件
vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => ({
  default: class {
    static isSupported() {
      return false;
    }
  },
}));

function renderPlayer() {
  return render(
    <MantineProvider>
      <VideoPlayer url="/dummy.mp4" streamType="mp4" autoPlay={false} />
    </MantineProvider>,
  );
}

describe('VideoPlayer 末端缓冲等待（FR-18）', () => {
  it('收到 waiting 事件展示「等待新数据…」横幅，canplay 后隐藏', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video')!;
    expect(video).toBeTruthy();
    await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));
    // 初始无等待
    expect(screen.queryByRole('status', { name: '缓冲等待' })).toBeNull();
    // 触发 waiting 事件 → 横幅出现
    act(() => {
      video.dispatchEvent(new Event('waiting'));
    });
    expect(screen.getByRole('status', { name: '缓冲等待' })).toBeInTheDocument();
    expect(screen.getByText('等待新数据…')).toBeInTheDocument();
    // canplay 复位 → 横幅消失
    act(() => {
      video.dispatchEvent(new Event('canplay'));
    });
    expect(screen.queryByRole('status', { name: '缓冲等待' })).toBeNull();
  });

  it('stalled 事件同样触发等待', async () => {
    const { container } = renderPlayer();
    const video = container.querySelector('video')!;
    await waitFor(() => expect(video.getAttribute('src')).toBe('/dummy.mp4'));
    act(() => {
      video.dispatchEvent(new Event('stalled'));
    });
    expect(screen.getByText('等待新数据…')).toBeInTheDocument();
  });
});
