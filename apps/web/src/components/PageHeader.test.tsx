import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import PageHeader from './PageHeader';

// FR-134 统一 PageHeader：默认隐藏冗余大标题（仅留无障碍语义），右侧操作区常驻；
// showTitle 时回到可见大标题。

function renderHeader(ui: React.ReactNode) {
  return render(<MantineProvider>{ui}</MantineProvider>);
}

describe('PageHeader（FR-134）', () => {
  it('默认隐藏大标题但保留无障碍可读文本（视觉隐藏样式）', () => {
    renderHeader(<PageHeader title="回收站" />);
    const title = screen.getByText('回收站');
    // 标题节点仍在 DOM（屏幕阅读器可读）
    expect(title).toBeInTheDocument();
    // 视觉隐藏：绝对定位 + 1px 裁剪
    expect(title.style.position).toBe('absolute');
    expect(title.style.width).toBe('1px');
  });

  it('showTitle=true 时显示可见大标题（不带视觉隐藏样式）', () => {
    renderHeader(<PageHeader title="相册" showTitle />);
    const title = screen.getByText('相册');
    expect(title).toBeInTheDocument();
    expect(title.style.position).toBe('');
  });

  it('渲染右侧操作区', () => {
    renderHeader(<PageHeader title="转码预设" actions={<button>新建预设</button>} />);
    expect(screen.getByRole('button', { name: '新建预设' })).toBeInTheDocument();
  });
});
