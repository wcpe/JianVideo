import { useState, useCallback, useEffect, useRef } from 'react'
import * as libApi from '@/api/library'
import type { MediaFile } from '@/types'

interface UseInfiniteMediaOptions {
  pageSize?: number
  sort?: string
}

/**
 * 累积分页加载媒体文件（用于时间轴滚动加载）。
 * 与 useMediaList 不同：每页结果是“追加”到 items，而非替换，
 * 以支持无限滚动；search 变化时重置并从第一页重新累积。
 */
export function useInfiniteMedia(options: UseInfiniteMediaOptions = {}) {
  const { pageSize = 60, sort = 'time_desc' } = options

  const [items, setItems] = useState<MediaFile[]>([])
  const [total, setTotal] = useState(0)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // 用 ref 持有最新的累积条数 / 总数 / 加载态 / 当前页，
  // 让 loadMore 读到实时值而非闭包快照（ref 仅在 effect 中写入）。
  // 当前页用 ref 维护而非 state，避免在重置 effect 中同步 setState。
  const itemsLenRef = useRef(0)
  const totalRef = useRef(0)
  const loadingRef = useRef(false)
  const pageRef = useRef(1)
  useEffect(() => { itemsLenRef.current = items.length }, [items])
  useEffect(() => { totalRef.current = total }, [total])
  useEffect(() => { loadingRef.current = loading }, [loading])

  // 拉取指定页：append=true 追加并按 id 去重，否则替换并视为新一轮累积。
  // 内部更新 pageRef，调用方无需关心页码状态。
  const fetchPage = useCallback(async (p: number, s: string, append: boolean) => {
    pageRef.current = p
    setLoading(true)
    setError(null)
    try {
      const res = await libApi.getMediaFiles({
        page: p,
        page_size: pageSize,
        search: s || undefined,
        sort,
      })
      setTotal(res.total)
      setItems((prev) => {
        if (!append) return res.items
        // 按 id 去重，避免分页边界或重复请求导致同一文件出现两次
        const seen = new Set(prev.map((m) => m.id))
        const appended = res.items.filter((m) => !seen.has(m.id))
        return appended.length > 0 ? [...prev, ...appended] : prev
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [pageSize, sort])

  // 搜索防抖 400ms，与 useMediaList 保持一致
  useEffect(() => {
    const timer = setTimeout(() => setSearch(searchInput), 400)
    return () => clearTimeout(timer)
  }, [searchInput])

  // search 变化时重置：回到第一页重新累积（append=false 会替换 items）。
  // 在 search 变化时主动发起首屏请求，属于与外部数据源同步的必要副作用。
  useEffect(() => {
    fetchPage(1, search, false)
  }, [search, fetchPage])

  // 加载下一页并追加；仅在仍有更多且非加载中时触发
  const loadMore = useCallback(() => {
    if (loadingRef.current || itemsLenRef.current >= totalRef.current) return
    fetchPage(pageRef.current + 1, search, true)
  }, [search, fetchPage])

  // 重新加载第一页（如扫描完成后刷新）
  const reload = useCallback(() => {
    fetchPage(1, search, false)
  }, [search, fetchPage])

  return {
    items,
    total,
    loading,
    error,
    searchInput,
    setSearchInput,
    setError,
    loadMore,
    hasMore: items.length < total,
    reload,
  }
}
