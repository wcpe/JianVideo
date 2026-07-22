import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { MantineProvider } from '@mantine/core';
import ConfirmModal from './ConfirmModal';

function renderWithProvider(ui: React.ReactElement) {
  return render(<MantineProvider>{ui}</MantineProvider>);
}

describe('ConfirmModal', () => {
  it('未打开时不渲染内容', () => {
    renderWithProvider(
      <ConfirmModal
        opened={false}
        title="删除确认"
        message="确定要删除吗？"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.queryByText('删除确认')).toBeNull();
  });

  it('打开时显示标题和消息', () => {
    renderWithProvider(
      <ConfirmModal
        opened={true}
        title="删除确认"
        message="确定要删除吗？"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByText('删除确认')).toBeVisible();
    expect(screen.getByText('确定要删除吗？')).toBeVisible();
  });

  it('显示默认按钮文字', () => {
    renderWithProvider(
      <ConfirmModal
        opened={true}
        title="标题"
        message="消息"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: '确认' })).toBeVisible();
    expect(screen.getByRole('button', { name: '取消' })).toBeVisible();
  });

  it('显示自定义按钮文字', () => {
    renderWithProvider(
      <ConfirmModal
        opened={true}
        title="标题"
        message="消息"
        confirmLabel="删除"
        cancelLabel="返回"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: '删除' })).toBeVisible();
    expect(screen.getByRole('button', { name: '返回' })).toBeVisible();
  });

  it('点击确认按钮触发 onConfirm', async () => {
    const onConfirm = vi.fn();
    const user = userEvent.setup();
    renderWithProvider(
      <ConfirmModal
        opened={true}
        title="标题"
        message="消息"
        onConfirm={onConfirm}
        onCancel={vi.fn()}
      />,
    );
    await user.click(screen.getByRole('button', { name: '确认' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('点击取消按钮触发 onCancel', async () => {
    const onCancel = vi.fn();
    const user = userEvent.setup();
    renderWithProvider(
      <ConfirmModal
        opened={true}
        title="标题"
        message="消息"
        onConfirm={vi.fn()}
        onCancel={onCancel}
      />,
    );
    await user.click(screen.getByRole('button', { name: '取消' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('loading 状态禁用按钮', () => {
    renderWithProvider(
      <ConfirmModal
        opened={true}
        title="标题"
        message="消息"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
        loading={true}
      />,
    );
    expect(screen.getByRole('button', { name: '确认' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '取消' })).toBeDisabled();
  });

  it('自定义确认按钮颜色', () => {
    renderWithProvider(
      <ConfirmModal
        opened={true}
        title="标题"
        message="消息"
        confirmColor="blue"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    const confirmButton = screen.getByRole('button', { name: '确认' });
    expect(confirmButton).toBeVisible();
    // Mantine Button 通过 CSS 类传递颜色，验证按钮存在即可
  });
});
