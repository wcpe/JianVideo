// 内容分级枚举与展示（FR2-051），与后端 models.ContentRating* 对齐。

/** 合法分级值（含 UNRATED；空串表示清除/未设） */
export const CONTENT_RATINGS = ['G', 'PG', 'PG-13', 'R', 'UNRATED'] as const;

export type ContentRating = (typeof CONTENT_RATINGS)[number];

/** 下拉选项：空=未分级；其余为枚举值 */
export const CONTENT_RATING_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '未分级' },
  { value: 'G', label: 'G · 全年龄' },
  { value: 'PG', label: 'PG · 建议家长指导' },
  { value: 'PG-13', label: 'PG-13 · 13 岁以上' },
  { value: 'R', label: 'R · 限制级' },
  { value: 'UNRATED', label: 'UNRATED · 未评级' },
];

/** Space/成员最高可见级选项：空=不限制 */
export const MAX_RATING_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '不限制' },
  { value: 'G', label: '最高 G' },
  { value: 'PG', label: '最高 PG' },
  { value: 'PG-13', label: '最高 PG-13' },
  { value: 'R', label: '最高 R' },
];

/** 展示用短标签；空/缺省 →「未分级」 */
export function formatContentRatingLabel(rating?: string | null): string {
  const r = (rating ?? '').trim();
  if (!r) return '未分级';
  const found = CONTENT_RATING_OPTIONS.find((o) => o.value === r);
  return found?.label ?? r;
}

/** Badge 颜色：R 偏警示，其余中性 */
export function contentRatingBadgeColor(rating?: string | null): string {
  const r = (rating ?? '').trim().toUpperCase();
  if (r === 'R') return 'red';
  if (r === 'PG-13' || r === 'PG13') return 'orange';
  if (r === 'PG') return 'yellow';
  if (r === 'G') return 'teal';
  return 'gray';
}
