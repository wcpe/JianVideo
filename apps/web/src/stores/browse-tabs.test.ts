import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useBrowseTabsStore, createBrowseTab } from './browse-tabs';
import { BROWSE_ROOT } from '@/hooks/useDirectoryBrowse';

// 模拟 settings API：记录读写，断言持久化往返（FR-151）
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

import { SETTING_KEY_OPEN_TABS, SETTING_KEY_LAST_OPENED_PATH } from '@/api/settings';

/** 把 store 复位为单一根标签的初始态，便于各用例独立。 */
function resetStore() {
  const tab = createBrowseTab(BROWSE_ROOT);
  useBrowseTabsStore.setState({
    tabs: [tab],
    activeTabId: tab.id,
    hydrated: false,
  });
}

describe('browse-tabs store', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUpdateSettings.mockResolvedValue({});
    resetStore();
  });

  it('初始为单一根标签且为激活态', () => {
    const { tabs, activeTabId } = useBrowseTabsStore.getState();
    expect(tabs).toHaveLength(1);
    expect(tabs[0].path).toBe(BROWSE_ROOT);
    expect(activeTabId).toBe(tabs[0].id);
  });

  it('addTab 在末尾新增标签并激活', () => {
    const before = useBrowseTabsStore.getState().tabs.length;
    useBrowseTabsStore.getState().addTab(BROWSE_ROOT);
    const { tabs, activeTabId } = useBrowseTabsStore.getState();
    expect(tabs).toHaveLength(before + 1);
    expect(activeTabId).toBe(tabs[tabs.length - 1].id);
  });

  it('setActiveTab 切换激活标签', () => {
    useBrowseTabsStore.getState().addTab('C:/a');
    const first = useBrowseTabsStore.getState().tabs[0].id;
    useBrowseTabsStore.getState().setActiveTab(first);
    expect(useBrowseTabsStore.getState().activeTabId).toBe(first);
  });

  it('closeTab 移除标签；关闭激活标签后激活相邻标签', () => {
    useBrowseTabsStore.getState().addTab('C:/a'); // 索引 1，激活
    const { tabs } = useBrowseTabsStore.getState();
    const activeId = tabs[1].id;
    useBrowseTabsStore.getState().closeTab(activeId);
    const after = useBrowseTabsStore.getState();
    expect(after.tabs.find((t) => t.id === activeId)).toBeUndefined();
    // 关闭后激活前一个相邻标签
    expect(after.activeTabId).toBe(tabs[0].id);
  });

  it('closeTab 不允许关闭最后一个标签', () => {
    const only = useBrowseTabsStore.getState().tabs[0].id;
    useBrowseTabsStore.getState().closeTab(only);
    expect(useBrowseTabsStore.getState().tabs).toHaveLength(1);
  });

  it('updateActiveTab 局部更新激活标签的状态', () => {
    useBrowseTabsStore.getState().updateActiveTab({ path: 'C:/photos', sort: 'size' });
    const tab = useBrowseTabsStore
      .getState()
      .tabs.find((t) => t.id === useBrowseTabsStore.getState().activeTabId)!;
    expect(tab.path).toBe('C:/photos');
    expect(tab.sort).toBe('size');
  });

  it('persist 把打开标签与上次位置写入 settings（FR-151）', async () => {
    useBrowseTabsStore.getState().updateActiveTab({ path: 'C:/photos' });
    await useBrowseTabsStore.getState().persist();

    expect(mockUpdateSettings).toHaveBeenCalledTimes(1);
    const arg = mockUpdateSettings.mock.calls[0][0] as Record<string, string>;
    expect(arg[SETTING_KEY_LAST_OPENED_PATH]).toBe('C:/photos');
    const openTabs = JSON.parse(arg[SETTING_KEY_OPEN_TABS]);
    expect(openTabs).toHaveLength(1);
    expect(openTabs[0].path).toBe('C:/photos');
  });

  it('hydrate 从 settings 恢复打开的标签（FR-151）', async () => {
    mockGetSettings.mockResolvedValue({
      [SETTING_KEY_OPEN_TABS]: JSON.stringify([
        { path: 'C:/a', sort: 'name', displayMode: 'details' },
        { path: 'C:/b', sort: 'size', displayMode: 'list' },
      ]),
      [SETTING_KEY_LAST_OPENED_PATH]: 'C:/b',
    });

    await useBrowseTabsStore.getState().hydrate();

    const { tabs, activeTabId, hydrated } = useBrowseTabsStore.getState();
    expect(hydrated).toBe(true);
    expect(tabs).toHaveLength(2);
    expect(tabs[0].path).toBe('C:/a');
    expect(tabs[1].path).toBe('C:/b');
    // 激活上次位置对应的标签
    expect(tabs.find((t) => t.id === activeTabId)?.path).toBe('C:/b');
  });

  it('hydrate 无持久化记录时保持默认单标签', async () => {
    mockGetSettings.mockResolvedValue({});

    await useBrowseTabsStore.getState().hydrate();

    const { tabs, hydrated } = useBrowseTabsStore.getState();
    expect(hydrated).toBe(true);
    expect(tabs).toHaveLength(1);
    expect(tabs[0].path).toBe(BROWSE_ROOT);
  });

  it('hydrate 容错：损坏的 open_tabs JSON 回退默认单标签', async () => {
    mockGetSettings.mockResolvedValue({ [SETTING_KEY_OPEN_TABS]: 'not-json' });

    await useBrowseTabsStore.getState().hydrate();

    const { tabs, hydrated } = useBrowseTabsStore.getState();
    expect(hydrated).toBe(true);
    expect(tabs).toHaveLength(1);
  });

  it('hydrate 只执行一次（已 hydrated 不再读 settings）', async () => {
    useBrowseTabsStore.setState({ hydrated: true });
    await useBrowseTabsStore.getState().hydrate();
    expect(mockGetSettings).not.toHaveBeenCalled();
  });
});
