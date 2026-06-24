/** 格式化文件大小为人类可读字符串 */
export function formatSize(bytes: number): string {
  if (bytes === 0) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

/** 格式化时长（秒）为 HH:MM:SS 或 MM:SS */
export function formatDuration(seconds: number): string {
  if (!seconds || seconds <= 0) return '-'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
  return `${m}:${s.toString().padStart(2, '0')}`
}

// 以下 EXIF 单位格式化（FR-106）：把后端存储的裸值归一化为标准摄影写法，
// 对已带/未带标准标记的输入均幂等，便于 EXIF 区一致呈现。

/**
 * 格式化光圈为 `f/1.8`（FR-106）。
 * 后端通常已存 `f/2.8`，但容错纯数字（如 `1.8` / `2`）补 `f/` 前缀；空值返回空串。
 */
export function formatAperture(raw?: string | null): string {
  const v = raw?.trim()
  if (!v) return ''
  // 已是 f/ 或 F/ 开头则归一化大小写后原样返回
  if (/^f\//i.test(v)) return `f/${v.slice(2)}`
  // 纯数值则补 f/ 前缀，否则原样返回（保留后端非常规写法）
  return /^\d+(\.\d+)?$/.test(v) ? `f/${v}` : v
}

/**
 * 格式化快门为 `1/200s`（FR-106）。
 * 后端通常已存 `1/60`（无后缀 s），补 `s`；已带 s 不重复；空值返回空串。
 */
export function formatShutter(raw?: string | null): string {
  const v = raw?.trim()
  if (!v) return ''
  if (/s$/i.test(v)) return v
  return `${v}s`
}

/**
 * 格式化 ISO 为 `ISO 100`（FR-106）。0 或缺失返回空串。
 */
export function formatIso(iso?: number | null): string {
  if (!iso || iso <= 0) return ''
  return `ISO ${iso}`
}
