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

/**
 * 取媒体「那天」的日期键 YYYY-MM-DD（FR-145）：供回忆卡片点击跳转那天用。
 * 时间源按 media_time → added_at → modified_at 降级取首个有效日期；皆非法返回空串。
 * 纯函数，无副作用。
 */
export function mediaDayKey(file: MediaFile): string {
  return pickValidDate(file.media_time, file.added_at, file.modified_at);
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

/**
 * 把日期查询规整为 YYYY-MM-DD 前缀比较基准（FR-142）。
 * - 接受 YYYY / YYYY-MM / YYYY-MM-DD（可含 `/` 或 `-` 分隔），按粒度补全到日：
 *   年补到当年最后一天、月补到当月最后一天，使「跳到某年/某月」落在该区间最新的一天。
 * - 非法输入返回空串。纯函数，无副作用。
 */
export function normalizeDateQuery(raw: string): string {
  const trimmed = raw.trim().replace(/\//g, '-');
  // 年 YYYY → 当年 12-31（区间右端，便于落到该年最新分组）
  if (/^\d{4}$/.test(trimmed)) return `${trimmed}-12-31`;
  // 月 YYYY-MM → 当月最后一天
  const month = /^(\d{4})-(\d{1,2})$/.exec(trimmed);
  if (month) {
    const y = month[1];
    const m = Number(month[2]);
    if (m < 1 || m > 12) return '';
    const lastDay = new Date(Number(y), m, 0).getDate(); // 第 0 天即上月末，等价当月最后一天
    return `${y}-${pad2(m)}-${pad2(lastDay)}`;
  }
  // 日 YYYY-MM-DD（允许个位月/日，补零规整）
  const day = /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(trimmed);
  if (day) {
    const m = Number(day[2]);
    const d = Number(day[3]);
    if (m < 1 || m > 12 || d < 1 || d > 31) return '';
    return `${day[1]}-${pad2(m)}-${pad2(d)}`;
  }
  return '';
}

/** 两位补零 */
function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

/** 把分组键（YYYY / YYYY-MM / YYYY-MM-DD）补全到其区间起始日 YYYY-MM-DD；非法返回空串 */
function groupStartKey(date: string): string {
  if (/^\d{4}$/.test(date)) return `${date}-01-01`; // 年 → 当年第一天
  if (/^\d{4}-\d{2}$/.test(date)) return `${date}-01`; // 月 → 当月第一天
  if (/^\d{4}-\d{2}-\d{2}$/.test(date)) return date; // 日 → 原样
  return '';
}

/**
 * 把日期查询解析为目标分组下标（FR-142）。
 * - 分组已倒序（顶部=最新=下标 0、底部=最旧）。
 * - 查询规整到其区间右端（normalizeDateQuery），分组取区间左端（起始日）比较：
 *   取「起始日不晚于查询区间末」的第一个（即最新的）分组——跳到某年/某月时
 *   落在该区间内最新的一组；按日查询落在覆盖该日的分组（无论分组粒度）。
 * - 若所有分组都晚于查询区间（查询早于全部数据），落到最旧的有效分组（末位）。
 * - 查询非法或无有效分组返回 -1（调用方据此不跳转）。
 * 纯函数，无副作用，便于穷举测试。
 */
export function resolveDateToGroupIndex(groups: DateGroup[], rawQuery: string): number {
  const target = normalizeDateQuery(rawQuery);
  if (!target) return -1;
  let lastValid = -1;
  for (let i = 0; i < groups.length; i++) {
    const start = groupStartKey(groups[i].date);
    if (!start) continue; // 跳过「未知日期」等非法分组
    lastValid = i;
    // 分组倒序，首个起始日 <= 查询区间末的分组即目标
    if (start <= target) return i;
  }
  // 查询早于全部数据：落到最旧的有效分组
  return lastValid;
}

/**
 * 取分组列表中给定下标处分组的日期键（FR-142 视口锁定用）。
 * 越界或空返回空串，便于调用方在粒度切换后据此重新定位。纯函数。
 */
export function groupDateAtIndex(groups: DateGroup[], index: number): string {
  if (index < 0 || index >= groups.length) return '';
  return groups[index].date;
}

/** 日期组排序：有效日期倒序，“未知日期”始终最后 */
function compareDateGroup(a: DateGroup, b: DateGroup): number {
  if (a.date === UNKNOWN_DATE) return 1;
  if (b.date === UNKNOWN_DATE) return -1;
  return a.date < b.date ? 1 : a.date > b.date ? -1 : 0;
}
