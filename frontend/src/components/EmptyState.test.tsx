import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { MantineProvider } from '@mantine/core';
import EmptyState from './EmptyState';

function renderWithProvider(ui: React.ReactElement) {
  return render(<MantineProvider>{ui}</MantineProvider>);
}

describe('EmptyState', () => {
  it('渲染标题与默认内置插画', () => {
    renderWithProvider(<EmptyState title="空空如也" />);
    expect(screen.getByText('空空如也')).toBeVisible();
    // 不传 icon 时渲染内置原创插画
    expect(screen.getByRole('img', { name: '空' })).toBeInTheDocument();
  });

  it('渲染说明文案', () => {
    renderWithProvider(<EmptyState title="标题" description="这里还没有内容" />);
    expect(screen.getByText('这里还没有内容')).toBeVisible();
  });

  it('传入自定义图标时不渲染内置插画', () => {
    renderWithProvider(
      <EmptyState title="标题" icon={<span data-testid="custom-icon">图标</span>} />,
    );
    expect(screen.getByTestId('custom-icon')).toBeInTheDocument();
    expect(screen.queryByRole('img', { name: '空' })).toBeNull();
  });

  it('不传 action 时不渲染按钮', () => {
    renderWithProvider(<EmptyState title="标题" />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('传入 action 时渲染按钮并点击触发回调', async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();
    renderWithProvider(<EmptyState title="标题" action={{ label: '去创建', onClick }} />);
    const btn = screen.getByRole('button', { name: '去创建' });
    expect(btn).toBeVisible();
    await user.click(btn);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('action loading 时按钮处于加载/禁用态', () => {
    renderWithProvider(
      <EmptyState title="标题" action={{ label: '处理中', onClick: vi.fn(), loading: true }} />,
    );
    expect(screen.getByRole('button', { name: '处理中' })).toHaveAttribute('data-loading', 'true');
  });
});
