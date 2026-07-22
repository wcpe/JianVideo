import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, vi } from 'vitest';

import BrowseTabBar from './BrowseTabBar';
import { createBrowseTab } from '@/stores/browse-tabs';
import { BROWSE_ROOT } from '@/hooks/useDirectoryBrowse';

function renderBar(props: Partial<React.ComponentProps<typeof BrowseTabBar>> = {}) {
  const rootTab = createBrowseTab(BROWSE_ROOT);
  const defaults: React.ComponentProps<typeof BrowseTabBar> = {
    tabs: [rootTab],
    activeTabId: rootTab.id,
    onSelect: vi.fn(),
    onClose: vi.fn(),
    onAdd: vi.fn(),
  };
  const merged = { ...defaults, ...props };
  render(
    <MantineProvider>
      <BrowseTabBar {...merged} />
    </MantineProvider>,
  );
  return merged;
}

describe('BrowseTabBar 标签栏（FR-150）', () => {
  it('根标签标题显示「全部」，非根标签取路径末段', () => {
    const root = createBrowseTab(BROWSE_ROOT);
    const sub = createBrowseTab('D:/Videos/Movies');
    renderBar({ tabs: [root, sub], activeTabId: root.id });
    expect(screen.getByText('全部')).toBeInTheDocument();
    expect(screen.getByText('Movies')).toBeInTheDocument();
  });

  it('点击标签触发 onSelect', async () => {
    const root = createBrowseTab(BROWSE_ROOT);
    const sub = createBrowseTab('D:/a');
    const props = renderBar({ tabs: [root, sub], activeTabId: root.id });
    await userEvent.click(screen.getByLabelText('标签 a'));
    expect(props.onSelect).toHaveBeenCalledWith(sub.id);
  });

  it('点击「+」触发 onAdd', async () => {
    const props = renderBar();
    await userEvent.click(screen.getByRole('button', { name: '新建标签' }));
    expect(props.onAdd).toHaveBeenCalledTimes(1);
  });

  it('多于一个标签时关闭按钮可用且触发 onClose（不冒泡触发切换）', async () => {
    const root = createBrowseTab(BROWSE_ROOT);
    const sub = createBrowseTab('D:/a');
    const props = renderBar({ tabs: [root, sub], activeTabId: root.id });
    await userEvent.click(screen.getByLabelText('关闭标签 a'));
    expect(props.onClose).toHaveBeenCalledWith(sub.id);
    // 关闭不应连带触发该标签的切换
    expect(props.onSelect).not.toHaveBeenCalled();
  });

  it('仅一个标签时关闭按钮禁用', () => {
    const root = createBrowseTab(BROWSE_ROOT);
    renderBar({ tabs: [root], activeTabId: root.id });
    expect(screen.getByLabelText('关闭标签 全部')).toBeDisabled();
  });
});
