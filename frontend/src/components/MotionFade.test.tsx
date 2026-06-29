import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

import MotionFade from './MotionFade';

// 通过 mock matchMedia 控制 prefers-reduced-motion，验证无障碍守护：
// 开启「减少动态」时，动效封装不应下发任何进入动画（initial/animate 关闭）。
function mockReducedMotion(reduce: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: query.includes('prefers-reduced-motion') ? reduce : false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
}

describe('MotionFade 动效封装（FR-135）', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('正常渲染子内容', () => {
    mockReducedMotion(false);
    render(<MotionFade>内容</MotionFade>);
    expect(screen.getByText('内容')).toBeInTheDocument();
  });

  it('默认（未减少动态）时容器带进入动画标记', () => {
    mockReducedMotion(false);
    render(<MotionFade data-testid="fade">内容</MotionFade>);
    // 启用动效时标记为动画态，供样式/断言钩子
    expect(screen.getByTestId('fade')).toHaveAttribute('data-motion', 'animate');
  });

  it('prefers-reduced-motion: reduce 下禁用进入动画（标记为 static）', () => {
    mockReducedMotion(true);
    render(<MotionFade data-testid="fade">内容</MotionFade>);
    expect(screen.getByTestId('fade')).toHaveAttribute('data-motion', 'static');
  });
});
