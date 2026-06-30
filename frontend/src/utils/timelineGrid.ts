import type { MediaFile } from '@/types';
import type { DateGroup } from './timeline';

/**
 * 时间轴网格的容器宽度断点 → 列数（FR-141）。
 * 与 TimelineView 网格 SimpleGrid 的 container 查询断点保持同一口径，
 * 供卡片级虚拟化按真实列数把每个日期组切成网格行。
 */
const COLUMN_BREAKPOINTS: ReadonlyArray<{ minWidth: number; cols: number }> = [
  { minWidth: 1360, cols: 8 },
  { minWidth: 1040, cols: 6 },
  { minWidth: 760, cols: 5 },
  { minWidth: 480, cols: 4 },
  { minWidth: 0, cols: 3 },
];

/** 受支持的多尺寸缩略图宽度白名单（与后端一致，FR-81）。 */
const THUMB_SIZES: ReadonlyArray<number> = [160, 320, 640];

/** 按容器宽度返回网格列数（FR-141），至少 1 列以避免后续除零/切片死循环。 */
export function gridColumnsForWidth(width: number): number {
  for (const bp of COLUMN_BREAKPOINTS) {
    if (width >= bp.minWidth) return bp.cols;
  }
  return 3;
}

/**
 * 按单卡逻辑宽度与设备像素比挑选缩略图请求尺寸（FR-141）。
 * 物理像素 = 列宽 * dpr，取白名单中不小于它的最小档；超出最大档则钳到最大档。
 * 非法入参（<=0）兜底为最小档，保证始终返回有效尺寸。
 */
export function thumbnailSizeForColumn(columnWidth: number, dpr: number): number {
  const physical = columnWidth > 0 && dpr > 0 ? columnWidth * dpr : 0;
  for (const size of THUMB_SIZES) {
    if (physical <= size) return size;
  }
  return THUMB_SIZES[THUMB_SIZES.length - 1];
}

/** 时间轴展平行：分组头行，或同一组内按列切出的一行卡片（FR-141）。 */
export type TimelineRow =
  | { type: 'header'; group: DateGroup }
  | { type: 'cards'; groupDate: string; files: MediaFile[] };

/** 展平结果：行列表 + 分组下标到其头行下标的映射（供 scrubber 跳转定位）。 */
export interface TimelineRowsResult {
  rows: TimelineRow[];
  groupIndexToRow: number[];
}

/**
 * 把按日期分组的结果展平为「头行 + 网格行」的一维行列表（FR-141）。
 * 卡片级虚拟化以「网格行」为虚拟单元：同一组内每 columns 张卡为一行，
 * 仅渲染视口附近的网格行，单日上千卡片也只渲染可见若干行。
 * groupIndexToRow 让右侧 scrubber 仍按「分组」跳转——映射到该组头行下标即可。
 */
export function buildTimelineRows(
  groups: DateGroup[],
  columns: number,
): TimelineRowsResult {
  const cols = Math.max(1, Math.floor(columns));
  const rows: TimelineRow[] = [];
  const groupIndexToRow: number[] = [];
  for (const group of groups) {
    groupIndexToRow.push(rows.length);
    rows.push({ type: 'header', group });
    for (let i = 0; i < group.files.length; i += cols) {
      rows.push({
        type: 'cards',
        groupDate: group.date,
        files: group.files.slice(i, i + cols),
      });
    }
  }
  return { rows, groupIndexToRow };
}
