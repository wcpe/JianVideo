import { create } from 'zustand';
import {
  getSettings,
  updateSettings,
  SETTING_KEY_OPEN_TABS,
  SETTING_KEY_LAST_OPENED_PATH,
} from '@/api/settings';
import { BROWSE_ROOT, type BrowseSort } from '@/hooks/useDirectoryBrowse';
import type { DisplayMode } from '@/components/DirectoryBrowser.helpers';

/** 媒体类型筛选取值（FR-36），空串表示不限。 */
export type BrowseMediaType = '' | 'image' | 'video';

/**
 * 单个浏览标签的状态（FR-150）：每标签独立目录位置与筛选/排序/展示态，
 * 类似 Windows 资源管理器一个标签 = 一个目录位置。
 */
export interface BrowseTab {
  /** 标签唯一 id（运行期生成，不持久化） */
  id: string;
  /** 当前目录位置（真实路径或 BROWSE_ROOT 根） */
  path: string;
  /** 排序键（FR-121） */
  sort: BrowseSort;
  /** 展示方式（FR-121） */
  displayMode: DisplayMode;
  /** 搜索表达式（FR-36） */
  search: string;
  /** 媒体类型筛选（FR-36） */
  mediaType: BrowseMediaType;
  /** 最小体积筛选（字节，0=不限） */
  sizeMin: number;
  /** 起始时间筛选（ISO 日期字符串，空=不限） */
  timeFrom: string;
  /** 截止时间筛选（ISO 日期字符串，空=不限） */
  timeTo: string;
}

/** 持久化到 settings 的标签快照（FR-151），仅保留稳定的位置与视图态。 */
interface PersistedTab {
  path: string;
  sort: BrowseSort;
  displayMode: DisplayMode;
}

let tabSeq = 0;

/** 生成一个带默认筛选/排序/展示态的新标签（FR-150）。 */
export function createBrowseTab(path: string = BROWSE_ROOT): BrowseTab {
  tabSeq += 1;
  return {
    id: `tab-${Date.now()}-${tabSeq}`,
    path,
    sort: 'name',
    displayMode: 'details',
    search: '',
    mediaType: '',
    sizeMin: 0,
    timeFrom: '',
    timeTo: '',
  };
}

interface BrowseTabsState {
  /** 打开的标签列表（FR-150），顺序即标签栏顺序 */
  tabs: BrowseTab[];
  /** 当前激活标签 id */
  activeTabId: string;
  /** 是否已从后端 settings 恢复过（FR-151），防重复 hydrate */
  hydrated: boolean;
  /** 新增标签并激活（FR-150） */
  addTab: (path?: string) => void;
  /** 关闭标签（FR-150）；不允许关闭最后一个；关闭激活标签后激活相邻标签 */
  closeTab: (id: string) => void;
  /** 切换激活标签（FR-150） */
  setActiveTab: (id: string) => void;
  /** 局部更新激活标签的状态（FR-150） */
  updateActiveTab: (patch: Partial<Omit<BrowseTab, 'id'>>) => void;
  /** 从后端 settings 恢复打开的标签与上次位置（FR-151），仅首次生效 */
  hydrate: () => Promise<void>;
  /** 把当前打开标签与上次位置写入后端 settings（FR-151） */
  persist: () => Promise<void>;
}

/** 取激活标签，找不到时回退到首个。 */
function activeOf(state: BrowseTabsState): BrowseTab {
  return state.tabs.find((t) => t.id === state.activeTabId) ?? state.tabs[0];
}

const initialTab = createBrowseTab(BROWSE_ROOT);

export const useBrowseTabsStore = create<BrowseTabsState>((set, get) => ({
  tabs: [initialTab],
  activeTabId: initialTab.id,
  hydrated: false,

  addTab: (path = BROWSE_ROOT) => {
    const tab = createBrowseTab(path);
    set((s) => ({ tabs: [...s.tabs, tab], activeTabId: tab.id }));
  },

  closeTab: (id) => {
    set((s) => {
      // 始终保留至少一个标签
      if (s.tabs.length <= 1) return s;
      const idx = s.tabs.findIndex((t) => t.id === id);
      if (idx < 0) return s;
      const tabs = s.tabs.filter((t) => t.id !== id);
      // 关闭的若是激活标签，激活前一个相邻标签（首个被关则取新首个）
      let activeTabId = s.activeTabId;
      if (id === s.activeTabId) {
        const nextIdx = Math.max(0, idx - 1);
        activeTabId = tabs[nextIdx].id;
      }
      return { tabs, activeTabId };
    });
  },

  setActiveTab: (id) => set({ activeTabId: id }),

  updateActiveTab: (patch) => {
    set((s) => ({
      tabs: s.tabs.map((t) => (t.id === s.activeTabId ? { ...t, ...patch } : t)),
    }));
  },

  hydrate: async () => {
    if (get().hydrated) return;
    try {
      const settings = await getSettings();
      const raw = settings[SETTING_KEY_OPEN_TABS];
      const lastPath = settings[SETTING_KEY_LAST_OPENED_PATH];
      const restored = parsePersistedTabs(raw);
      if (restored.length > 0) {
        const tabs = restored.map((p) => {
          const t = createBrowseTab(p.path);
          return { ...t, sort: p.sort, displayMode: p.displayMode };
        });
        // 激活上次浏览位置对应的标签，匹配不到则激活首个
        const active = (lastPath && tabs.find((t) => t.path === lastPath)) || tabs[0];
        set({ tabs, activeTabId: active.id, hydrated: true });
        return;
      }
    } catch {
      // 恢复失败不阻塞浏览：保持默认单标签，仅标记已尝试
    }
    set({ hydrated: true });
  },

  persist: async () => {
    const s = get();
    const persisted: PersistedTab[] = s.tabs.map((t) => ({
      path: t.path,
      sort: t.sort,
      displayMode: t.displayMode,
    }));
    await updateSettings({
      [SETTING_KEY_OPEN_TABS]: JSON.stringify(persisted),
      [SETTING_KEY_LAST_OPENED_PATH]: activeOf(s).path,
    });
  },
}));

/** 解析持久化的标签 JSON（FR-151），容错损坏数据，过滤非法项。 */
function parsePersistedTabs(raw: string | undefined): PersistedTab[] {
  if (!raw) return [];
  try {
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return [];
    return arr
      .filter((x): x is PersistedTab => !!x && typeof x.path === 'string')
      .map((x) => ({
        path: x.path,
        sort: (x.sort as BrowseSort) || 'name',
        displayMode: (x.displayMode as DisplayMode) || 'details',
      }));
  } catch {
    return [];
  }
}
