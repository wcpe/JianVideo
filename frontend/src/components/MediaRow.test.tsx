import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { MantineProvider } from '@mantine/core';
import MediaRow from './MediaRow';

function renderRow(ui: React.ReactNode) {
  return render(<MantineProvider>{ui}</MantineProvider>);
}

describe('MediaRow（统一媒体行，FR-101）', () => {
  it('渲染主标题与副标题', () => {
    renderRow(<MediaRow mediaID={1} title="片子.mkv" subtitle="源文件丢失" />);
    expect(screen.getByText('片子.mkv')).toBeVisible();
    expect(screen.getByText('源文件丢失')).toBeVisible();
  });

  it('有 mediaID 时渲染缩略图（复用 MediaThumbnail，img 始终在 DOM）', () => {
    renderRow(<MediaRow mediaID={9} thumbFileName="a.mkv" title="a.mkv" />);
    // MediaThumbnail 用 alt=fileName 渲染 <img>
    expect(screen.getByRole('img', { name: 'a.mkv' })).toBeInTheDocument();
  });

  it('无 mediaID 时不渲染缩略图（巡检页源文件丢失场景）', () => {
    renderRow(<MediaRow title="丢了.mp4" subtitle="源文件丢失" />);
    expect(screen.queryByRole('img')).toBeNull();
  });

  it('勾选模式渲染复选框并回调 onToggle', async () => {
    const onToggle = vi.fn();
    const user = userEvent.setup();
    renderRow(
      <MediaRow
        title="多余.jpg"
        selectable
        selected={false}
        onToggle={onToggle}
        checkboxLabel="选择 多余.jpg"
      />,
    );
    const checkbox = screen.getByRole('checkbox', { name: '选择 多余.jpg' });
    await user.click(checkbox);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('渲染右侧操作区', () => {
    renderRow(<MediaRow mediaID={1} title="待还原.mkv" actions={<button>还原</button>} />);
    expect(screen.getByRole('button', { name: '还原' })).toBeVisible();
  });

  it('根节点带 .mantine-Card-root 类（兼容既有页面以 closest 定位）', () => {
    const { container } = renderRow(<MediaRow mediaID={1} title="x.mkv" />);
    expect(container.querySelector('.mantine-Card-root')).not.toBeNull();
  });
});
