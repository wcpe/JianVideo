import { useState, useCallback, useRef, useEffect } from 'react'
import * as libApi from '@/api/library'
import type { MediaFile, BreadcrumbItem, DirInfo } from '@/types'

export function useDirectoryBrowse() {
  const [browseLibraryID, setBrowseLibraryID] = useState<number | null>(null)
  const [browseParentPath, setBrowseParentPath] = useState('/')
  const [breadcrumbs, setBreadcrumbs] = useState<BreadcrumbItem[]>([])
  const [directories, setDirectories] = useState<DirInfo[]>([])
  const [files, setFiles] = useState<MediaFile[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const initialized = useRef(false)

  const loadBrowse = useCallback(async (libraryID: number, parentPath: string) => {
    setLoading(true)
    setError(null)
    try {
      const res = await libApi.browseDirectory(libraryID, parentPath)
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

  // 当选中的路径变化时，自动加载目录浏览（仅在首次初始化时设置默认路径）
  const initIfNeeded = useCallback((libraryID: number, parentPath: string) => {
    if (!initialized.current) {
      setBrowseLibraryID(libraryID)
      setBrowseParentPath(parentPath)
      initialized.current = true
    }
  }, [])

  const handleEnterDir = useCallback((path: string) => {
    setBrowseParentPath(path)
  }, [])

  const handleBreadcrumbNavigate = useCallback((path: string) => {
    setBrowseParentPath(path)
  }, [])

  const handleBrowsePath = useCallback((libraryID: number, parentPath: string) => {
    setBrowseLibraryID(libraryID)
    setBrowseParentPath(parentPath)
    initialized.current = true
  }, [])

  // 当 browseLibraryID 或 browseParentPath 变化时加载
  useEffect(() => {
    if (browseLibraryID !== null) {
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
    initIfNeeded,
    handleEnterDir,
    handleBreadcrumbNavigate,
    handleBrowsePath,
  }
}
