import { useState, useCallback, useRef } from 'react'
import { notifications } from '@mantine/notifications'
import * as libApi from '@/api/library'
import type { LibraryPath, MediaExtensionType } from '@/types'

export function useLibraryPaths(
  onPathsChanged?: () => void,
) {
  const [paths, setPaths] = useState<LibraryPath[]>([])
  const [loading, setLoading] = useState(false)
  const [newPath, setNewPath] = useState('')
  const [addingPath, setAddingPath] = useState(false)
  const addingPathRef = useRef(false)
  const [scanLoading, setScanLoading] = useState<Record<number, boolean>>({})
  const [extensionInputs, setExtensionInputs] = useState<Record<number, string>>({})
  const [extensionTypes, setExtensionTypes] = useState<Record<number, MediaExtensionType>>({})
  const [extensionLoading, setExtensionLoading] = useState<Record<number, boolean>>({})
  const [customImageExtensions, setCustomImageExtensions] = useState<Record<number, string[]>>({})

  const loadExtensionPolicies = useCallback(async (items: LibraryPath[], replace = true) => {
    const entries = await Promise.all(items.map(async (path) => {
      try {
        const extensions = await libApi.listMediaExtensions(path.id)
        const imageExts = extensions
          .filter(ext => ext.type === 'image')
          .map(ext => ext.extension.toLowerCase().replace(/^\./, ''))
        return [path.id, imageExts] as const
      } catch {
        return [path.id, []] as const
      }
    }))
    const next = Object.fromEntries(entries)
    setCustomImageExtensions(prev => (replace ? next : { ...prev, ...next }))
  }, [])

  const loadPaths = useCallback(async () => {
    setLoading(true)
    try {
      const items = await libApi.getLibraryPaths()
      setPaths(items)
      await loadExtensionPolicies(items)
    } catch {
      // 错误由上层处理
    } finally {
      setLoading(false)
    }
  }, [loadExtensionPolicies])

  const handleAddPath = useCallback(async () => {
    if (!newPath.trim() || addingPathRef.current) return
    addingPathRef.current = true
    setAddingPath(true)
    try {
      const created = await libApi.createLibraryPath(newPath.trim())
      setNewPath('')
      notifications.show({ title: '添加成功', message: `目录 "${created.label || created.path}" 已添加`, color: 'green', autoClose: 3000 })
      await loadPaths()
      await onPathsChanged?.()
    } catch (err) {
      const message = err instanceof Error ? err.message : '无法添加目录，请检查路径是否正确'
      notifications.show({ title: '添加失败', message, color: 'red', autoClose: 3000 })
    } finally {
      addingPathRef.current = false
      setAddingPath(false)
    }
  }, [newPath, loadPaths, onPathsChanged])

  const handleDeletePath = useCallback(async (path: LibraryPath, onConfirm?: () => Promise<void>) => {
    try {
      await onConfirm?.()
      notifications.show({ title: '删除成功', message: `目录 "${path.label || path.path}" 已删除`, color: 'green', autoClose: 3000 })
      await loadPaths()
      await onPathsChanged?.()
    } catch {
      notifications.show({ title: '删除失败', message: '无法删除目录', color: 'red', autoClose: 3000 })
    }
  }, [loadPaths, onPathsChanged])

  const handleScan = useCallback(async (id: number, onScanDone?: () => Promise<void>) => {
    setScanLoading(prev => ({ ...prev, [id]: true }))
    try {
      const res = await libApi.scanLibrary(id)
      notifications.show({ title: '扫描完成', message: `发现 ${res.scanned} 个新文件`, color: 'green', autoClose: 3000 })
      await onPathsChanged?.()
      await onScanDone?.()
    } catch {
      notifications.show({ title: '扫描失败', message: '扫描目录时出错', color: 'red', autoClose: 3000 })
    } finally {
      setScanLoading(prev => ({ ...prev, [id]: false }))
    }
  }, [onPathsChanged])

  const handleAddExtension = useCallback(async (path: LibraryPath) => {
    const extension = (extensionInputs[path.id] || '').trim()
    if (!extension) return
    const mediaType = extensionTypes[path.id] || 'video'
    setExtensionLoading(prev => ({ ...prev, [path.id]: true }))
    try {
      await libApi.addMediaExtension(path.id, extension, mediaType)
      setExtensionInputs(prev => ({ ...prev, [path.id]: '' }))
      await loadExtensionPolicies([path], false)
      notifications.show({ title: '后缀已添加', message: `${extension} 已绑定到 "${path.label || path.path}"`, color: 'green', autoClose: 3000 })
    } catch (err) {
      const message = err instanceof Error ? err.message : '添加后缀失败，请检查格式'
      notifications.show({ title: '添加失败', message, color: 'red', autoClose: 3000 })
    } finally {
      setExtensionLoading(prev => ({ ...prev, [path.id]: false }))
    }
  }, [extensionInputs, extensionTypes, loadExtensionPolicies])

  return {
    // 状态
    paths,
    loading,
    newPath,
    addingPath,
    scanLoading,
    extensionInputs,
    extensionTypes,
    extensionLoading,
    customImageExtensions,
    // setter
    setNewPath,
    setExtensionInputs,
    setExtensionTypes,
    // 操作
    loadPaths,
    handleAddPath,
    handleDeletePath,
    handleScan,
    handleAddExtension,
  }
}
