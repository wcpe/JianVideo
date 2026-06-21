import type { MediaFile } from '@/types'

const BUILT_IN_IMAGES = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg']

/** 媒体展示名（FR-30）：优先用库内显示名 display_name，为空或仅空白时回退真实文件名 file_name。 */
export function mediaDisplayName(file: MediaFile): string {
  const name = file.display_name?.trim()
  return name ? name : file.file_name
}

/** 判断媒体文件是否为图片（含自定义图片后缀） */
export function isImageFile(file: MediaFile, customImageExtensions: Record<number, string[]> = {}): boolean {
  const format = file.format.toLowerCase().replace(/^\./, '')
  if (BUILT_IN_IMAGES.includes(format)) return true
  const custom = customImageExtensions[file.library_id] || []
  return custom.includes(format)
}
