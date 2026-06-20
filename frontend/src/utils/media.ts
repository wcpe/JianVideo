import type { MediaFile } from '@/types'

const BUILT_IN_IMAGES = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg']

/** 判断媒体文件是否为图片（含自定义图片后缀） */
export function isImageFile(file: MediaFile, customImageExtensions: Record<number, string[]> = {}): boolean {
  const format = file.format.toLowerCase().replace(/^\./, '')
  if (BUILT_IN_IMAGES.includes(format)) return true
  const custom = customImageExtensions[file.library_id] || []
  return custom.includes(format)
}
