import { useState, useCallback, useEffect, useRef } from 'react';
import * as libApi from '@/api/library';
import type { MediaFile } from '@/types';

interface UseInfiniteMediaOptions {
  pageSize?: number;
  sort?: string;
  // 收藏/标签筛选（FR-41）：favorite=true 仅收藏，tagId>0 仅含该标签
  favorite?: boolean;
  tagId?: number;
  // 媒体库（图库）筛选（FR-144）：libraryId>0 仅含该库媒体，0 表示全部图库不过滤
  libraryId?: number;
  // 结构化筛选（FR-35/36）：类型 / 最小大小（字节）/ 拍摄时间范围（YYYY-MM-DD）
  mediaType?: 'image' | 'video' | '';
  sizeMin?: number;
  timeFrom?: string;
  timeTo?: string;
  // FR2-046：最短时长（秒）/ 最小高度（像素）
  durationMin?: number;
  heightMin?: number;
  // 本地影视信息状态筛选（FR2-031）
  inference?: 'inferred' | 'auto' | 'manual' | 'missing' | '';
  // 初始搜索关键词（FR-132）：用于承接页眉全局搜索经 URL ?search= 传入，
  // 作为搜索框初值并使首屏即以该词请求；仅作初值，后续由用户输入接管。
  initialSearch?: string;
}

/**
 * 累积分页加载媒体文件（用于时间轴滚动加载）。
 * 每页结果“追加”到 items 而非替换，以支持无限滚动；
 * search 变化时重置并从第一页重新累积。
 */
export function useInfiniteMedia(options: UseInfiniteMediaOptions = {}) {
  const {
    pageSize = 60,
    sort = 'time_desc',
    favorite = false,
    tagId = 0,
    libraryId = 0,
    mediaType = '',
    sizeMin = 0,
    timeFrom = '',
    timeTo = '',
    durationMin = 0,
    heightMin = 0,
    inference = '',
    initialSearch = '',
  } = options;

  const [items, setItems] = useState<MediaFile[]>([]);
  const [total, setTotal] = useState(0);
  // 搜索初值取自 initialSearch（FR-132 承接页眉全局搜索）：首屏即按该词请求，搜索框回填该词
  const [search, setSearch] = useState(initialSearch);
  const [searchInput, setSearchInput] = useState(initialSearch);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 用 ref 持有最新的累积条数 / 总数 / 加载态 / 当前页，
  // 让 loadMore 读到实时值而非闭包快照（ref 仅在 effect 中写入）。
  // 当前页用 ref 维护而非 state，避免在重置 effect 中同步 setState。
  const itemsLenRef = useRef(0);
  const totalRef = useRef(0);
  // 同步的“拉取中”标记：在 fetchPage 起始处立即置位，供 loadMore 立刻判重，
  // 避免用滞后于 render 的 loading state 导致滚动哨兵快速触发时重复拉取。
  const fetchingRef = useRef(false);
  const pageRef = useRef(1);
  useEffect(() => {
    itemsLenRef.current = items.length;
  }, [items]);
  useEffect(() => {
    totalRef.current = total;
  }, [total]);

  // 拉取指定页：append=true 追加并按 id 去重，否则替换并视为新一轮累积。
  // 内部更新 pageRef，调用方无需关心页码状态。
  const fetchPage = useCallback(
    async (p: number, s: string, append: boolean) => {
      fetchingRef.current = true;
      pageRef.current = p;
      setLoading(true);
      setError(null);
      try {
        const res = await libApi.getMediaFiles({
          page: p,
          page_size: pageSize,
          search: s || undefined,
          sort,
          favorite: favorite || undefined,
          tag_id: tagId || undefined,
          library_id: libraryId || undefined,
          type: mediaType || undefined,
          size_min: sizeMin || undefined,
          time_from: timeFrom || undefined,
          time_to: timeTo || undefined,
          duration_min: durationMin || undefined,
          height_min: heightMin || undefined,
          inference: inference || undefined,
        });
        setTotal(res.total);
        setItems((prev) => {
          if (!append) return res.items;
          // 按 id 去重，避免分页边界或重复请求导致同一文件出现两次
          const seen = new Set(prev.map((m) => m.id));
          const appended = res.items.filter((m) => !seen.has(m.id));
          return appended.length > 0 ? [...prev, ...appended] : prev;
        });
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载失败');
      } finally {
        setLoading(false);
        fetchingRef.current = false;
      }
    },
    [
      pageSize,
      sort,
      favorite,
      tagId,
      libraryId,
      mediaType,
      sizeMin,
      timeFrom,
      timeTo,
      durationMin,
      heightMin,
      inference,
    ],
  );

  // 搜索防抖 400ms
  useEffect(() => {
    const timer = setTimeout(() => setSearch(searchInput), 400);
    return () => clearTimeout(timer);
  }, [searchInput]);

  // search 变化时重置：回到第一页重新累积（append=false 会替换 items）。
  // 在 search 变化时主动发起首屏请求，属于与外部数据源同步的必要副作用。
  useEffect(() => {
    fetchPage(1, search, false);
  }, [search, fetchPage]);

  // 加载下一页并追加；仅在仍有更多且无拉取进行中时触发
  const loadMore = useCallback(() => {
    if (fetchingRef.current || itemsLenRef.current >= totalRef.current) return;
    fetchPage(pageRef.current + 1, search, true);
  }, [search, fetchPage]);

  // 重新加载第一页（如扫描完成后刷新）
  const reload = useCallback(() => {
    fetchPage(1, search, false);
  }, [search, fetchPage]);

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
  };
}
