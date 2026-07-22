import { useEffect, useState, useCallback } from 'react';
import MemoryCarousel from '@/components/MemoryCarousel';
import MemoryCard from '@/components/MemoryCard';
import * as libApi from '@/api/library';
import type { MediaFile } from '@/types';

/**
 * 时间轴页「最近查看」回忆区块（FR-120 + FR-145）。
 *
 * 拉取最近打开过的媒体（图片 + 视频，按 last_viewed_at 倒序），横向轮播展示缩略图卡片
 * 与展示名（左右滚动按钮 + scroll-snap）；卡片主体点击跳到那天并按日期筛选时间轴，
 * 卡片提供独立播放入口（FR-145）。列表为空（或拉取失败）时整块不渲染。
 *
 * 与「继续观看」（FR-44，有进度未看完）维度不同：本区块按最近打开排序、不论进度/类型，
 * 二者可重叠展示、互不替代。
 */
export default function RecentlyViewed() {
  const [items, setItems] = useState<MediaFile[]>([]);

  const load = useCallback(() => {
    libApi
      .getRecentlyViewed()
      .then(setItems)
      .catch(() => setItems([]));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  if (items.length === 0) return null;

  return (
    <MemoryCarousel title="最近查看" testId="recently-viewed">
      {items.map((f) => (
        <MemoryCard key={f.id} file={f} labelPrefix="最近查看" />
      ))}
    </MemoryCarousel>
  );
}
