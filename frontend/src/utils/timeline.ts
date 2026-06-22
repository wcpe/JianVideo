import type { MediaFile } from '@/types'

/** 一个日期分组：date 为分组键（年 YYYY / 月 YYYY-MM / 日 YYYY-MM-DD），无效归入“未知日期” */
export interface DateGroup {
  date: string
  files: MediaFile[]
}

/** 时间轴分组粒度（FR-32 缩放）：日 / 月 / 年 */
export type TimelineGranularity = 'day' | 'month' | 'year'

/** 未知日期分组标识，始终排在最后 */
const UNKNOWN_DATE = '未知日期'

/**
 * 按时间对媒体文件分组（FR-32）。
 * - 时间源优先 media_time（FR-31 媒体时间），缺失回退 added_at。
 * - 粒度 granularity 决定分组键：day=YYYY-MM-DD / month=YYYY-MM / year=YYYY（缩放）。
 * - 空值或格式非法归入“未知日期”组（始终最后）；组按键倒序。
 * - 组内保持输入顺序（调用方已按 media_time 倒序）。
 */
export function groupMediaByDate(files: MediaFile[], granularity: TimelineGranularity = 'day'): DateGroup[] {
  const groups = new Map<string, MediaFile[]>()
  for (const file of files) {
    const date = extractDateKey(file, granularity)
    const bucket = groups.get(date)
    if (bucket) {
      bucket.push(file)
    } else {
      groups.set(date, [file])
    }
  }
  return Array.from(groups.entries())
    .map(([date, groupFiles]) => ({ date, files: groupFiles }))
    .sort(compareDateGroup)
}

/** 按粒度提取日期键；时间源 media_time 优先、回退 added_at；非法返回“未知日期” */
function extractDateKey(file: MediaFile, granularity: TimelineGranularity): string {
  const raw = file.media_time || file.added_at
  if (!raw || raw.length < 10) return UNKNOWN_DATE
  const date = raw.slice(0, 10)
  // 校验 YYYY-MM-DD 形态，避免把脏数据当成有效日期
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return UNKNOWN_DATE
  switch (granularity) {
    case 'year':
      return date.slice(0, 4) // YYYY
    case 'month':
      return date.slice(0, 7) // YYYY-MM
    default:
      return date // YYYY-MM-DD
  }
}

/** 日期组排序：有效日期倒序，“未知日期”始终最后 */
function compareDateGroup(a: DateGroup, b: DateGroup): number {
  if (a.date === UNKNOWN_DATE) return 1
  if (b.date === UNKNOWN_DATE) return -1
  return a.date < b.date ? 1 : a.date > b.date ? -1 : 0
}
