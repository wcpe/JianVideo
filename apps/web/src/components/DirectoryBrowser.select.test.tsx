import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, vi } from 'vitest';

import DirectoryBrowser from './DirectoryBrowser';
import type { MediaFile, DirInfo } from '@/types';

function file(over: Partial<MediaFile>): MediaFile {
  return {
    id: 1,
    library_id: 1,
    file_path: 'D:/m/a.jpg',
    file_name: 'a.jpg',
    file_size: 1000,
    format: 'jpg',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 0,
    height: 0,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z',
    modified_at: '2025-01-01T00:00:00Z',
    ...over,
  } as MediaFile;
}

function renderBrowser(props: Partial<React.ComponentProps<typeof DirectoryBrowser>>) {
  return render(
    <MantineProvider>
      <DirectoryBrowser
        breadcrumbs={[]}
        directories={[]}
        files={[]}
        loading={false}
        error={null}
        customImageExtensions={{}}
        onEnterDir={() => {}}
        onBreadcrumbNavigate={() => {}}
        onErrorClose={() => {}}
        onOpenFile={() => {}}
        displayMode="list"
        sort="name"
        {...props}
      />
    </MantineProvider>,
  );
}

const threeFiles = [
  file({ id: 1, file_name: 'a.jpg' }),
  file({ id: 2, file_name: 'b.jpg' }),
  file({ id: 3, file_name: 'c.jpg' }),
];

describe('DirectoryBrowser 多选上抛与右键菜单（FR-69）', () => {
  it('选中变化上抛 onSelectionChange', () => {
    const onSelectionChange = vi.fn();
    renderBrowser({ files: threeFiles, onSelectionChange });
    fireEvent.click(screen.getByText('a.jpg'));
    // 最近一次回调应包含选中的 a(id=1)
    const last = onSelectionChange.mock.calls.at(-1)![0] as number[];
    expect(last).toEqual([1]);
  });

  it('右键弹出菜单，「删除选中」按选中批量调 onBatchDelete', async () => {
    const onBatchDelete = vi.fn();
    renderBrowser({ files: threeFiles, onBatchDelete });
    // 选中 a、Shift 选到 c → a,b,c
    fireEvent.click(screen.getByText('a.jpg'));
    fireEvent.click(screen.getByText('c.jpg'), { shiftKey: true });
    // 右键 b 卡片（菜单异步挂载，用 findByText 等待）
    fireEvent.contextMenu(screen.getByText('b.jpg'));
    await userEvent.click(await screen.findByText('删除选中（3）'));
    expect(onBatchDelete).toHaveBeenCalledWith([1, 2, 3]);
  });

  it('未选中时右键「删除」退化为删该右键项 onDeleteOne', async () => {
    const onDeleteOne = vi.fn();
    const onBatchDelete = vi.fn();
    renderBrowser({ files: threeFiles, onDeleteOne, onBatchDelete });
    fireEvent.contextMenu(screen.getByText('b.jpg'));
    // 无选中时菜单显示「删除」并退化为删该右键项
    await userEvent.click(await screen.findByText('删除'));
    expect(onDeleteOne).toHaveBeenCalledWith(expect.objectContaining({ id: 2 }));
    expect(onBatchDelete).not.toHaveBeenCalled();
  });

  it('右键「全选」选中全部并上抛', async () => {
    const onSelectionChange = vi.fn();
    renderBrowser({ files: threeFiles, onSelectionChange });
    fireEvent.contextMenu(screen.getByText('a.jpg'));
    await userEvent.click(await screen.findByText('全选'));
    const last = onSelectionChange.mock.calls.at(-1)![0] as number[];
    expect([...last].sort((x, y) => x - y)).toEqual([1, 2, 3]);
  });

  it('无选择相关回调时仍可正常渲染与高亮（不回归）', () => {
    renderBrowser({ files: threeFiles });
    fireEvent.click(screen.getByText('a.jpg'));
    const card = screen.getByText('a.jpg').closest('.mantine-Card-root');
    expect(card).toHaveAttribute('data-selected');
  });
});

// 目录项不应被选中（仅文件可选，保持现状）
describe('DirectoryBrowser 目录不可选（FR-69）', () => {
  it('右键目录卡片不弹媒体菜单', () => {
    renderBrowser({
      directories: [{ name: '子目录', path: 'D:/m/sub' } as DirInfo],
      files: [file({ id: 1, file_name: 'a.jpg' })],
      onBatchDelete: vi.fn(),
    });
    fireEvent.contextMenu(screen.getByText('子目录'));
    // 目录右键不出现「删除选中」「全选」等媒体菜单项
    expect(screen.queryByText('全选')).not.toBeInTheDocument();
  });
});
