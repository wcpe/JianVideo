import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { MantineProvider } from '@mantine/core';
import SelectionBatchBar from './SelectionBatchBar';

function renderBar(props: Partial<React.ComponentProps<typeof SelectionBatchBar>> = {}) {
  return render(
    <MantineProvider>
      <SelectionBatchBar count={props.count ?? 2} {...props} />
    </MantineProvider>,
  );
}

describe('SelectionBatchBar（FR-99 sticky 批量条）', () => {
  it('count=0 时不渲染批量条', () => {
    renderBar({ count: 0 });
    expect(screen.queryByRole('toolbar', { name: '批量操作' })).toBeNull();
  });

  it('count≥1 时渲染并显示已选数量', () => {
    renderBar({ count: 3 });
    expect(screen.getByText(/已选\s*3\s*项/)).toBeTruthy();
  });

  it('提供批量回调时渲染对应按钮并可触发', async () => {
    const onDelete = vi.fn();
    const onAddToAlbum = vi.fn();
    const onAddTag = vi.fn();
    const onDownload = vi.fn();
    const onClear = vi.fn();
    renderBar({ count: 2, onDelete, onAddToAlbum, onAddTag, onDownload, onClear });

    await userEvent.click(screen.getByRole('button', { name: '加入相册' }));
    expect(onAddToAlbum).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole('button', { name: '打标签' }));
    expect(onAddTag).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole('button', { name: '打包下载' }));
    expect(onDownload).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole('button', { name: '删除' }));
    expect(onDelete).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole('button', { name: '清除选择' }));
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it('未提供某批量回调时不渲染该按钮', () => {
    renderBar({ count: 2, onDelete: vi.fn(), onClear: vi.fn() });
    expect(screen.queryByRole('button', { name: '加入相册' })).toBeNull();
    expect(screen.queryByRole('button', { name: '打标签' })).toBeNull();
    expect(screen.queryByRole('button', { name: '打包下载' })).toBeNull();
    // 删除与清除始终在
    expect(screen.getByRole('button', { name: '删除' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '清除选择' })).toBeTruthy();
  });
});
