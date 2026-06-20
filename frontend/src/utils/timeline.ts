import type { MediaFile } from '@/types'

/** 一个日期分组：date 为 YYYY-MM-DD（无效日期归入“未知日期”） */
export interface DateGroup {
  date: string
  files: MediaFile[]
}

/** 未知日期分组标识，始终排在最后 */
const UNKNOWN_DATE = '未知日期'

/**
 * 按 added_at 的日期（YYYY-MM-DD）对媒体文件分组。
 * - 取 ISO 字符串前 10 位作为日期键，空值或格式非法归入“未知日期”组。
 * - 组按日期倒序排列，“未知日期”组始终最后。
 * - 组内保持输入顺序（调用方已按 time_desc 排序）。
 */
export function groupMediaByDate(files: MediaFile[]): DateGroup[] {
  const groups = new Map<string, MediaFile[]>()
  for (const file of files) {
    const date = extractDate(file.added_at)
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

/** 提取 YYYY-MM-DD 日期键，非法值返回“未知日期” */
function extractDate(addedAt: string | undefined | null): string {
  if (!addedAt || addedAt.length < 10) return UNKNOWN_DATE
  const date = addedAt.slice(0, 10)
  // 校验 YYYY-MM-DD 形态，避免把脏数据当成有效日期
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return UNKNOWN_DATE
  return date
}

/** 日期组排序：有效日期倒序，“未知日期”始终最后 */
function compareDateGroup(a: DateGroup, b: DateGroup): number {
  if (a.date === UNKNOWN_DATE) return 1
  if (b.date === UNKNOWN_DATE) return -1
  return a.date < b.date ? 1 : a.date > b.date ? -1 : 0
}
