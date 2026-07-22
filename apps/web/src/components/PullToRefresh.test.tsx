import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import PullToRefresh from './PullToRefresh';

// FR-143 移动端下拉刷新：仅在页面顶部、向下拖动超过阈值后松手触发 onRefresh。
// 非顶部、拖动不足、向上拖动均不触发。桌面（disabled）完全不绑定手势。

function renderPTR(props: Partial<React.ComponentProps<typeof PullToRefresh>> = {}) {
  const onRefresh = props.onRefresh ?? vi.fn().mockResolvedValue(undefined);
  const utils = render(
    <MantineProvider>
      <PullToRefresh onRefresh={onRefresh} enabled {...props}>
        <div data-testid="ptr-child">内容</div>
      </PullToRefresh>
    </MantineProvider>,
  );
  return { ...utils, onRefresh };
}

/** 桩化 window.scrollY，便于模拟「页面顶部 / 非顶部」 */
function setScrollY(y: number) {
  Object.defineProperty(window, 'scrollY', { value: y, writable: true, configurable: true });
}

describe('PullToRefresh（FR-143）', () => {
  beforeEach(() => setScrollY(0));
  afterEach(() => vi.clearAllMocks());

  it('渲染子内容', () => {
    renderPTR();
    expect(screen.getByTestId('ptr-child')).toBeInTheDocument();
  });

  it('页面顶部向下拖动超阈值后松手触发 onRefresh', async () => {
    const { onRefresh } = renderPTR();
    const region = screen.getByTestId('pull-to-refresh');
    // 起手在顶部
    fireEvent.touchStart(region, { touches: [{ clientY: 0 }] });
    // 向下拖动 120px（超过阈值 70）
    fireEvent.touchMove(region, { touches: [{ clientY: 120 }] });
    await act(async () => {
      fireEvent.touchEnd(region, { changedTouches: [{ clientY: 120 }] });
    });
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it('向下拖动不足阈值不触发', async () => {
    const { onRefresh } = renderPTR();
    const region = screen.getByTestId('pull-to-refresh');
    fireEvent.touchStart(region, { touches: [{ clientY: 0 }] });
    fireEvent.touchMove(region, { touches: [{ clientY: 30 }] });
    await act(async () => {
      fireEvent.touchEnd(region, { changedTouches: [{ clientY: 30 }] });
    });
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it('非页面顶部（已滚动）不触发下拉刷新', async () => {
    setScrollY(200);
    const { onRefresh } = renderPTR();
    const region = screen.getByTestId('pull-to-refresh');
    fireEvent.touchStart(region, { touches: [{ clientY: 0 }] });
    fireEvent.touchMove(region, { touches: [{ clientY: 120 }] });
    await act(async () => {
      fireEvent.touchEnd(region, { changedTouches: [{ clientY: 120 }] });
    });
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it('disabled（桌面）时下拉不触发刷新', async () => {
    const onRefresh = vi.fn().mockResolvedValue(undefined);
    render(
      <MantineProvider>
        <PullToRefresh onRefresh={onRefresh} enabled={false}>
          <div>内容</div>
        </PullToRefresh>
      </MantineProvider>,
    );
    const region = screen.getByTestId('pull-to-refresh');
    fireEvent.touchStart(region, { touches: [{ clientY: 0 }] });
    fireEvent.touchMove(region, { touches: [{ clientY: 120 }] });
    await act(async () => {
      fireEvent.touchEnd(region, { changedTouches: [{ clientY: 120 }] });
    });
    expect(onRefresh).not.toHaveBeenCalled();
  });
});
