import { useEffect, useState, useCallback } from 'react';
import MemoryCarousel from '@/components/MemoryCarousel';
import MemoryCard from '@/components/MemoryCard';
import * as libApi from '@/api/library';
import type { MediaFile } from '@/types';

/**
 * 计算「X 年前的今天」文案（FR-72）：今年减媒体年份。
 * media_time 为空或无法解析时返回空串（不展示标签）。
 */
function yearsAgoLabel(mediaTime?: string | null): string {
  if (!mediaTime) return '';
  const t = new Date(mediaTime);
  if (Number.isNaN(t.getTime())) return '';
  const diff = new Date().getFullYear() - t.getFullYear();
  return diff > 0 ? `${diff} 年前的今天` : '';
}

/**
 * 首页「那年今日」回忆区块（FR-72 + FR-145）。
 *
 * 拉取往年同一天（媒体时间命中今天月-日、年份非今年）拍摄的媒体，横向轮播展示缩略图卡片
 * （左右滚动按钮 + scroll-snap），每项标注「X 年前的今天」；卡片主体点击跳到那天并按日期
 * 筛选时间轴，卡片提供独立播放入口（FR-145）。列表为空时整块不渲染。
 */
export default function OnThisDay() {
  const [items, setItems] = useState<MediaFile[]>([]);

  const load = useCallback(() => {
    libApi
      .getOnThisDay()
      .then(setItems)
      .catch(() => setItems([]));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  if (items.length === 0) return null;

  return (
    <MemoryCarousel title="那年今日" testId="on-this-day">
      {items.map((f) => (
        <MemoryCard
          key={f.id}
          file={f}
          labelPrefix="那年今日"
          badge={yearsAgoLabel(f.media_time)}
        />
      ))}
    </MemoryCarousel>
  );
}
