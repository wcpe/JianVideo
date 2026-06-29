// DirectoryBrowser 的展示 / 排序类型与纯逻辑（FR-33/FR-121）：从组件文件拆出，
// 使 DirectoryBrowser.tsx 仅导出组件，满足 react-refresh/only-export-components。
import { mediaDisplayName } from '@/utils/media';
import type { MediaFile } from '@/types';

/** 展示方式（FR-33/FR-121）：详情表格 / 列表 / 大-中-小图标 */
export type DisplayMode = 'details' | 'list' | 'large' | 'medium' | 'small';
/** 排序方式（FR-33） */
export type DirSort = 'name' | 'size' | 'type' | 'time';

/** 按排序方式对文件排序（纯函数，不改输入）。 */
export function sortFiles(files: MediaFile[], sort: DirSort): MediaFile[] {
  const arr = [...files];
  switch (sort) {
    case 'size':
      return arr.sort((a, b) => a.file_size - b.file_size);
    case 'type':
      return arr.sort(
        (a, b) =>
          (a.format || '').localeCompare(b.format || '') || a.file_name.localeCompare(b.file_name),
      );
    case 'time':
      return arr.sort((a, b) => (a.modified_at || '').localeCompare(b.modified_at || ''));
    default:
      return arr.sort((a, b) => mediaDisplayName(a).localeCompare(mediaDisplayName(b)));
  }
}
