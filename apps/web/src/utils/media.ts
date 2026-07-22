import type { MediaFile, MediaInference } from '@/types';

const BUILT_IN_IMAGES = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg'];
const HIGH_CONFIDENCE_THRESHOLD = 0.75;

/** 媒体展示名（FR2-031）：人工推断优先，其次 display_name、高置信自动推断、真实文件名。 */
export function mediaDisplayName(file: MediaFile, inference?: MediaInference | null): string {
  const resolvedInference = inference === undefined ? file.inference : inference;
  const manualTitle = resolvedInference?.manual ? resolvedInference.title.trim() : '';
  if (manualTitle) return manualTitle;
  const name = file.display_name?.trim();
  if (name) return name;
  const autoTitle =
    resolvedInference &&
    !resolvedInference.manual &&
    resolvedInference.confidence >= HIGH_CONFIDENCE_THRESHOLD
      ? resolvedInference.title.trim()
      : '';
  return autoTitle || file.file_name;
}

/** 判断媒体文件是否为图片（含自定义图片后缀） */
export function isImageFile(
  file: MediaFile,
  customImageExtensions: Record<number, string[]> = {},
): boolean {
  const format = file.format.toLowerCase().replace(/^\./, '');
  if (BUILT_IN_IMAGES.includes(format)) return true;
  const custom = customImageExtensions[file.library_id] || [];
  return custom.includes(format);
}
