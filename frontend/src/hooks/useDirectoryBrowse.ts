import { useState, useCallback, useRef, useEffect } from 'react';
import * as libApi from '@/api/library';
import type { MediaFile, BreadcrumbItem, DirInfo } from '@/types';

/** 真实路径树根标记（FR-121）：顶层列出各盘符 / 共享根。 */
export const BROWSE_ROOT = '__root__';

/** 目录排序键（FR-121）：服务端按此排序文件（目录恒在前、按名）。 */
export type BrowseSort = 'name' | 'size' | 'type' | 'time';

/**
 * 目录浏览数据 hook（FR-121 真实路径树）：仅按真实路径 parent_path 跨库导航，
 * 排序经后端 sort 参数生效（name/size/type/time）。不再依赖 library_id。
 */
export function useDirectoryBrowse(initialSort: BrowseSort = 'name') {
  const [currentPath, setCurrentPath] = useState<string>(BROWSE_ROOT);
  const [sort, setSortState] = useState<BrowseSort>(initialSort);
  const [breadcrumbs, setBreadcrumbs] = useState<BreadcrumbItem[]>([]);
  const [directories, setDirectories] = useState<DirInfo[]>([]);
  const [files, setFiles] = useState<MediaFile[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 是否已初始化过浏览起点（真实路径或根），避免查询参数副作用重复初始化
  const initialized = useRef(false);

  const loadBrowse = useCallback(async (path: string, sortKey: BrowseSort) => {
    setLoading(true);
    setError(null);
    try {
      const res = await libApi.browseDirectory(path, sortKey);
      setBreadcrumbs(res.breadcrumbs);
      setDirectories(res.directories);
      setFiles(res.files);
    } catch (err) {
      const message = err instanceof Error ? err.message : '加载目录内容失败，请重试';
      setError(message);
      setBreadcrumbs([]);
      setDirectories([]);
      setFiles([]);
    } finally {
      setLoading(false);
    }
  }, []);

  // 以真实路径树根初始化（无定位查询参数时）。仅首次生效。
  const initRoot = useCallback(() => {
    if (initialized.current) return;
    initialized.current = true;
    setCurrentPath(BROWSE_ROOT);
  }, []);

  // 带定位（起始 path）直接进该目录（如存储库管理页跳转）。library_id 已弃用、忽略。
  const initPath = useCallback((path: string) => {
    initialized.current = true;
    setCurrentPath(path);
  }, []);

  // 进入子目录 / 切换路径（地址栏、左树、目录卡片共用）。
  const navigateTo = useCallback((path: string) => {
    setCurrentPath(path);
  }, []);

  const handleEnterDir = useCallback((dir: DirInfo) => {
    setCurrentPath(dir.path);
  }, []);

  // 切换排序（FR-121）：服务端重排，随之重载当前目录。
  const setSort = useCallback((next: BrowseSort) => {
    setSortState(next);
  }, []);

  // 当前路径或排序变化即重载。
  useEffect(() => {
    loadBrowse(currentPath, sort);
  }, [currentPath, sort, loadBrowse]);

  // 手动重载当前目录（如删除后刷新）。
  const reload = useCallback(() => {
    loadBrowse(currentPath, sort);
  }, [currentPath, sort, loadBrowse]);

  return {
    currentPath,
    sort,
    breadcrumbs,
    directories,
    files,
    loading,
    error,
    setError,
    setSort,
    navigateTo,
    handleEnterDir,
    initRoot,
    initPath,
    reload,
  };
}
