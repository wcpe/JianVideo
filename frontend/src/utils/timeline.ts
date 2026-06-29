import type { MediaFile } from '@/types';

/** 一个日期分组：date 为分组键（年 YYYY / 月 YYYY-MM / 日 YYYY-MM-DD），无效归入“未知日期” */
export interface DateGroup {
  date: string;
  files: MediaFile[];
}

/** 时间轴分组粒度（FR-32 缩放 + FR-120 扩展）：日 / 月 / 年 / 所有（不分组、方形密铺） */
export type TimelineGranularity = 'day' | 'month' | 'year' | 'all';

/** 未知日期分组标识，始终排在最后 */
const UNKNOWN_DATE = '未知日期';

/** “所有”粒度的单组键（FR-120）：不按日期分组，全部媒体并入一组方形密铺 */
const ALL_GROUP = '全部';

/**
 * 按时间对媒体文件分组（FR-32）。
 * - 时间源按降级链 media_time（FR-31 媒体时间）→ added_at → modified_at 取第一个有效日期。
 * - 粒度 granularity 决定分组键：day=YYYY-MM-DD / month=YYYY-MM / year=YYYY（缩放）。
 * - 三个时间源全部缺失/格式非法才归入“未知日期”组（始终最后）；组按键倒序。
 * - 组内保持输入顺序（调用方已按 media_time 倒序）。
 */
export function groupMediaByDate(
  files: MediaFile[],
  granularity: TimelineGranularity = 'day',
): DateGroup[] {
  // “所有”粒度（FR-120）：不按日期分组，全部并入单组、保持输入顺序（调用方已倒序）；空输入返回空数组
  if (granularity === 'all') {
    return files.length === 0 ? [] : [{ date: ALL_GROUP, files: [...files] }];
  }
  const groups = new Map<string, MediaFile[]>();
  for (const file of files) {
    const date = extractDateKey(file, granularity);
    const bucket = groups.get(date);
    if (bucket) {
      bucket.push(file);
    } else {
      groups.set(date, [file]);
    }
  }
  return Array.from(groups.entries())
    .map(([date, groupFiles]) => ({ date, files: groupFiles }))
    .sort(compareDateGroup);
}

/** 按粒度提取日期键；时间源按 media_time → added_at → modified_at 降级取首个有效日期；皆非法返回“未知日期” */
function extractDateKey(file: MediaFile, granularity: TimelineGranularity): string {
  // 降级链：依次尝试媒体时间、入库时间、修改时间，取第一个解析出有效 YYYY-MM-DD 的源
  const date = pickValidDate(file.media_time, file.added_at, file.modified_at);
  if (!date) return UNKNOWN_DATE;
  switch (granularity) {
    case 'year':
      return date.slice(0, 4); // YYYY
    case 'month':
      return date.slice(0, 7); // YYYY-MM
    default:
      return date; // YYYY-MM-DD
  }
}

/** 从候选时间串中取第一个有效的日期（YYYY-MM-DD 前缀），无则返回空串 */
function pickValidDate(...candidates: (string | null | undefined)[]): string {
  for (const raw of candidates) {
    if (!raw || raw.length < 10) continue;
    const date = raw.slice(0, 10);
    // 校验 YYYY-MM-DD 形态，避免把脏数据当成有效日期
    if (/^\d{4}-\d{2}-\d{2}$/.test(date)) return date;
  }
  return '';
}

/**
 * 把 scrubber 指针纵向比例映射到目标分组下标（FR-68）。
 * - fraction：指针在轨道内的纵向比例，0=顶部=最新分组、1=底部=最旧分组；超界先钳制到 [0,1]。
 * - groupCount：分组总数；<=0 返回 0。
 * - 把 [0,1] 均分成 groupCount 段，返回所落段下标，钳制到 [0, groupCount-1]。
 * 纯函数，无副作用。
 */
export function positionToGroupIndex(fraction: number, groupCount: number): number {
  if (groupCount <= 0) return 0;
  const clamped = Math.min(1, Math.max(0, fraction));
  const index = Math.floor(clamped * groupCount);
  // fraction=1 时 floor 结果会等于 groupCount，需钳到最后一段
  return Math.min(groupCount - 1, index);
}

/** 日期组排序：有效日期倒序，“未知日期”始终最后 */
function compareDateGroup(a: DateGroup, b: DateGroup): number {
  if (a.date === UNKNOWN_DATE) return 1;
  if (b.date === UNKNOWN_DATE) return -1;
  return a.date < b.date ? 1 : a.date > b.date ? -1 : 0;
}
