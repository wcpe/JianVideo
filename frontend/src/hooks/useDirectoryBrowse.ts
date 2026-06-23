import { useState, useCallback, useRef, useEffect } from 'react'
import * as libApi from '@/api/library'
import type { MediaFile, BreadcrumbItem, DirInfo } from '@/types'

/** 目录浏览聚合虚拟根标记（FR-66）：列出所有启用库作为顶层目录。 */
export const BROWSE_ROOT = '__root__'

export function useDirectoryBrowse() {
  const [browseLibraryID, setBrowseLibraryID] = useState<number | null>(null)
  const [browseParentPath, setBrowseParentPath] = useState(BROWSE_ROOT)
  const [breadcrumbs, setBreadcrumbs] = useState<BreadcrumbItem[]>([])
  const [directories, setDirectories] = useState<DirInfo[]>([])
  const [files, setFiles] = useState<MediaFile[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // 是否已初始化过浏览起点（聚合虚拟根或带库定位），避免查询参数副作用重复初始化
  const initialized = useRef(false)

  const loadBrowse = useCallback(async (libraryID: number | null, parentPath: string) => {
    setLoading(true)
    setError(null)
    try {
      // 聚合根（FR-66）不带 library_id；其余传 0 占位由 api 层按 >0 决定是否携带
      const res = await libApi.browseDirectory(libraryID ?? 0, parentPath)
      setBreadcrumbs(res.breadcrumbs)
      setDirectories(res.directories)
      setFiles(res.files)
    } catch (err) {
      const message = err instanceof Error ? err.message : '加载目录内容失败，请重试'
      setError(message)
      setBreadcrumbs([])
      setDirectories([])
      setFiles([])
    } finally {
      setLoading(false)
    }
  }, [])

  // 以聚合虚拟根初始化（无库定位查询参数时）。仅首次生效。
  const initRoot = useCallback(() => {
    if (initialized.current) return
    initialized.current = true
    setBrowseLibraryID(null)
    setBrowseParentPath(BROWSE_ROOT)
  }, [])

  // 进入目录（FR-66）：根层库目录项带 library_id → 切到该库下钻；普通子目录仅切路径。
  const handleEnterDir = useCallback((dir: DirInfo) => {
    if (dir.library_id != null && dir.library_id > 0) {
      setBrowseLibraryID(dir.library_id)
    }
    setBrowseParentPath(dir.path)
  }, [])

  // 面包屑导航：回到虚拟根 → 清空当前库；否则在当前库内切路径。
  const handleBreadcrumbNavigate = useCallback((path: string) => {
    if (path === BROWSE_ROOT) {
      setBrowseLibraryID(null)
    }
    setBrowseParentPath(path)
  }, [])

  // 带库定位（library_id + 起始 path）直接进库浏览（如存储库管理页跳转）。
  const handleBrowsePath = useCallback((libraryID: number, parentPath: string) => {
    initialized.current = true
    setBrowseLibraryID(libraryID)
    setBrowseParentPath(parentPath)
  }, [])

  // 加载：聚合根（path=__root__）忽略 library_id 始终加载；其余有库才加载。
  useEffect(() => {
    if (browseParentPath === BROWSE_ROOT || browseLibraryID !== null) {
      loadBrowse(browseLibraryID, browseParentPath)
    }
  }, [browseLibraryID, browseParentPath, loadBrowse])

  return {
    browseLibraryID,
    browseParentPath,
    breadcrumbs,
    directories,
    files,
    loading,
    error,
    setError,
    setBrowseParentPath,
    loadBrowse,
    initRoot,
    handleEnterDir,
    handleBreadcrumbNavigate,
    handleBrowsePath,
  }
}
