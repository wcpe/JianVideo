import { useState, useCallback, useEffect } from 'react'
import * as libApi from '@/api/library'
import type { MediaFile } from '@/types'

interface UseMediaListOptions {
  pageSize?: number
  sort?: string
}

export function useMediaList(options: UseMediaListOptions = {}) {
  const { pageSize = 20, sort = 'time_desc' } = options

  const [mediaFiles, setMediaFiles] = useState<MediaFile[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadMedia = useCallback(async (p: number, s: string) => {
    setLoading(true)
    setError(null)
    try {
      const res = await libApi.getMediaFiles({
        page: p,
        page_size: pageSize,
        search: s || undefined,
        sort,
      })
      setMediaFiles(res.items)
      setTotal(res.total)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
      setMediaFiles([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [pageSize, sort])

  // 搜索防抖 400ms
  useEffect(() => {
    const timer = setTimeout(() => setSearch(searchInput), 400)
    return () => clearTimeout(timer)
  }, [searchInput])

  // search 变化时重置页码
  useEffect(() => {
    setPage(1)
  }, [search])

  useEffect(() => {
    loadMedia(page, search)
  }, [page, search, loadMedia])

  return {
    mediaFiles,
    total,
    page,
    search,
    searchInput,
    loading,
    error,
    setPage,
    setSearchInput,
    setError,
    loadMedia,
    totalPages: Math.max(1, Math.ceil(total / pageSize)),
  }
}
