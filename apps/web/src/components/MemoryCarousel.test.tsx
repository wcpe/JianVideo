import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import MemoryCarousel from './MemoryCarousel';

// FR-145 回忆轮播：横向轮播容器，左右滚动按钮 + CSS scroll-snap。
// 按钮调用滚动容器的 scrollBy 横向翻页；容器不可横向滚动时按钮禁用。

function renderCarousel(scrollWidth = 1200, clientWidth = 400) {
  const view = render(
    <MantineProvider>
      <MemoryCarousel title="最近查看" testId="recently-viewed">
        <div style={{ width: 1200 }}>内容</div>
      </MemoryCarousel>
    </MantineProvider>,
  );
  // jsdom 不计算布局，手动桩 scrollWidth/clientWidth 模拟可横向滚动
  const track = screen.getByTestId('carousel-track');
  Object.defineProperty(track, 'scrollWidth', { value: scrollWidth, configurable: true });
  Object.defineProperty(track, 'clientWidth', { value: clientWidth, configurable: true });
  Object.defineProperty(track, 'scrollLeft', { value: 0, writable: true, configurable: true });
  return { view, track };
}

describe('MemoryCarousel 回忆轮播（FR-145）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('渲染标题与内容', () => {
    renderCarousel();
    expect(screen.getByText('最近查看')).toBeInTheDocument();
    expect(screen.getByText('内容')).toBeInTheDocument();
  });

  it('滚动轨道启用 scroll-snap', () => {
    const { track } = renderCarousel();
    expect(track).toHaveStyle({ scrollSnapType: 'x mandatory' });
  });

  it('点击右滚按钮调用 scrollBy 正向翻页', async () => {
    const { track } = renderCarousel();
    const scrollBy = vi.fn();
    track.scrollBy = scrollBy as unknown as Element['scrollBy'];
    // 触发一次 scroll 评估按钮可用性（scrollLeft=0、可右滚 → 右按钮可用）
    act(() => track.dispatchEvent(new Event('scroll')));
    const right = await screen.findByRole('button', { name: '向右滚动' });
    await waitFor(() => expect(right).not.toBeDisabled());
    await userEvent.click(right);
    expect(scrollBy).toHaveBeenCalled();
    const arg = scrollBy.mock.calls[0][0] as ScrollToOptions;
    expect(arg.left).toBeGreaterThan(0);
  });

  it('点击左滚按钮调用 scrollBy 反向翻页', async () => {
    const { track } = renderCarousel();
    const scrollBy = vi.fn();
    track.scrollBy = scrollBy as unknown as Element['scrollBy'];
    // 已向右滚动一段（scrollLeft>0 → 左按钮可用）
    Object.defineProperty(track, 'scrollLeft', { value: 400, writable: true, configurable: true });
    act(() => track.dispatchEvent(new Event('scroll')));
    const left = await screen.findByRole('button', { name: '向左滚动' });
    await waitFor(() => expect(left).not.toBeDisabled());
    await userEvent.click(left);
    expect(scrollBy).toHaveBeenCalled();
    const arg = scrollBy.mock.calls[0][0] as ScrollToOptions;
    expect(arg.left).toBeLessThan(0);
  });
});
