import { useEffect, useRef } from 'react';
import { useBrowseTabsStore } from './browse-tabs';

/** 持久化写入防抖间隔（毫秒）：合并连续标签操作，避免频繁请求后端 settings。 */
const PERSIST_DEBOUNCE_MS = 500;

/**
 * 浏览标签持久化接线（FR-151）：挂载时从后端 settings 恢复打开的标签与上次位置，
 * 恢复完成后，标签列表或上次位置变化即防抖写回后端（复用 settings 键 open_tabs/last_opened_path）。
 * hydrate 内部以 hydrated 守卫防重复；持久化仅在 hydrate 完成后启用，避免恢复前的空写覆盖。
 */
export function useBrowseTabsPersistence(): void {
  const hydrate = useBrowseTabsStore((s) => s.hydrate);
  const persist = useBrowseTabsStore((s) => s.persist);
  const tabs = useBrowseTabsStore((s) => s.tabs);
  const activeTabId = useBrowseTabsStore((s) => s.activeTabId);
  const hydrated = useBrowseTabsStore((s) => s.hydrated);

  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // 挂载时恢复一次（store 内 hydrated 守卫保证幂等）
  useEffect(() => {
    void hydrate();
  }, [hydrate]);

  // 恢复完成后，标签结构/位置变化即防抖持久化
  useEffect(() => {
    if (!hydrated) return;
    clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      void persist();
    }, PERSIST_DEBOUNCE_MS);
    return () => clearTimeout(timerRef.current);
  }, [tabs, activeTabId, hydrated, persist]);
}
