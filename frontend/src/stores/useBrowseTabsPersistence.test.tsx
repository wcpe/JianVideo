import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { useBrowseTabsStore, createBrowseTab } from './browse-tabs';
import { BROWSE_ROOT } from '@/hooks/useDirectoryBrowse';

// 模拟 settings API：记录读写，验证 FR-151 恢复与持久化接线
const mockGetSettings = vi.fn();
const mockUpdateSettings = vi.fn();

vi.mock('@/api/settings', async () => {
  const actual = await vi.importActual<typeof import('@/api/settings')>('@/api/settings');
  return {
    ...actual,
    getSettings: (...args: unknown[]) => mockGetSettings(...args),
    updateSettings: (...args: unknown[]) => mockUpdateSettings(...args),
  };
});

import { useBrowseTabsPersistence } from './useBrowseTabsPersistence';
import { SETTING_KEY_OPEN_TABS, SETTING_KEY_LAST_OPENED_PATH } from '@/api/settings';

function resetStore() {
  const tab = createBrowseTab(BROWSE_ROOT);
  useBrowseTabsStore.setState({ tabs: [tab], activeTabId: tab.id, hydrated: false });
}

// 仅挂载 persistence hook 的探针组件
function Probe() {
  useBrowseTabsPersistence();
  return null;
}

describe('useBrowseTabsPersistence（FR-151 接线）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useRealTimers();
    mockGetSettings.mockResolvedValue({});
    mockUpdateSettings.mockResolvedValue({});
    resetStore();
  });

  it('挂载时调用一次 hydrate 从 settings 恢复', async () => {
    mockGetSettings.mockResolvedValue({
      [SETTING_KEY_OPEN_TABS]: JSON.stringify([
        { path: 'C:/a', sort: 'name', displayMode: 'details' },
        { path: 'C:/b', sort: 'size', displayMode: 'list' },
      ]),
      [SETTING_KEY_LAST_OPENED_PATH]: 'C:/b',
    });

    render(<Probe />);

    await waitFor(() => {
      const { tabs, hydrated } = useBrowseTabsStore.getState();
      expect(hydrated).toBe(true);
      expect(tabs).toHaveLength(2);
    });
    expect(mockGetSettings).toHaveBeenCalledTimes(1);
  });

  it('hydrate 完成后标签结构变化触发持久化（防抖）', async () => {
    render(<Probe />);
    // 等 hydrate 完成
    await waitFor(() => expect(useBrowseTabsStore.getState().hydrated).toBe(true));
    mockUpdateSettings.mockClear();

    // 新增一个标签 → 防抖后写入 settings
    useBrowseTabsStore.getState().addTab('C:/photos');

    await waitFor(() => expect(mockUpdateSettings).toHaveBeenCalled(), { timeout: 2000 });
    const arg = mockUpdateSettings.mock.calls.at(-1)![0] as Record<string, string>;
    const openTabs = JSON.parse(arg[SETTING_KEY_OPEN_TABS]);
    expect(openTabs).toHaveLength(2);
    expect(arg[SETTING_KEY_LAST_OPENED_PATH]).toBe('C:/photos');
  });

  it('hydrate 完成前不触发持久化', async () => {
    // hydrate 故意悬挂未决，期间改动 store 不应 persist
    let resolveGet: (v: Record<string, string>) => void = () => {};
    mockGetSettings.mockReturnValue(
      new Promise<Record<string, string>>((r) => {
        resolveGet = r;
      }),
    );

    render(<Probe />);
    useBrowseTabsStore.getState().addTab('C:/x');
    // 给防抖与微任务一点时间
    await new Promise((r) => setTimeout(r, 50));
    expect(mockUpdateSettings).not.toHaveBeenCalled();

    resolveGet({});
  });
});
