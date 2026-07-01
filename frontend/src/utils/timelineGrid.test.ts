import { describe, it, expect } from 'vitest';
import { gridColumnsForWidth, thumbnailSizeForColumn, buildTimelineRows } from './timelineGrid';
import type { DateGroup } from './timeline';
import type { MediaFile } from '@/types';

const file = (id: number): MediaFile => ({
  id,
  library_id: 1,
  file_path: `/x/f${id}.png`,
  file_name: `f${id}.png`,
  file_size: 1,
  format: 'png',
  video_codec: '',
  audio_codec: '',
  duration: 0,
  width: 1,
  height: 1,
  bitrate: 0,
  subtitle_tracks: '',
  added_at: '2025-01-09T00:00:00Z',
  modified_at: '2025-01-09T00:00:00Z',
});

describe('gridColumnsForWidth（FR-141 列数随容器宽度自适应）', () => {
  it('按断点返回列数，与时间轴网格容器查询口径一致', () => {
    // 断点：>=1360→8、>=1040→6、>=760→5、>=480→4、其余→3
    expect(gridColumnsForWidth(0)).toBe(3);
    expect(gridColumnsForWidth(300)).toBe(3);
    expect(gridColumnsForWidth(480)).toBe(4);
    expect(gridColumnsForWidth(760)).toBe(5);
    expect(gridColumnsForWidth(1040)).toBe(6);
    expect(gridColumnsForWidth(1360)).toBe(8);
    expect(gridColumnsForWidth(2000)).toBe(8);
  });

  it('列数至少为 1，避免除零', () => {
    expect(gridColumnsForWidth(-100)).toBeGreaterThanOrEqual(1);
  });
});

describe('thumbnailSizeForColumn（FR-141 缩略图尺寸按列宽/DPR 自适应）', () => {
  it('列宽越大选越大档（160/320/640 白名单）', () => {
    // 物理像素 = 列宽 * dpr，向上取最近的不小于它的白名单档，超 640 钳到 640
    expect(thumbnailSizeForColumn(120, 1)).toBe(160);
    expect(thumbnailSizeForColumn(160, 1)).toBe(160);
    expect(thumbnailSizeForColumn(200, 1)).toBe(320);
    expect(thumbnailSizeForColumn(320, 1)).toBe(320);
    expect(thumbnailSizeForColumn(500, 1)).toBe(640);
    expect(thumbnailSizeForColumn(1000, 1)).toBe(640);
  });

  it('高 DPR（如 2x）按物理像素选更大档', () => {
    // 160 逻辑像素在 2x 屏 = 320 物理像素
    expect(thumbnailSizeForColumn(160, 2)).toBe(320);
    expect(thumbnailSizeForColumn(200, 2)).toBe(640);
  });

  it('非法 dpr/列宽兜底为最小档', () => {
    expect(thumbnailSizeForColumn(0, 1)).toBe(160);
    expect(thumbnailSizeForColumn(100, 0)).toBe(160);
  });
});

describe('buildTimelineRows（FR-141 展平为「头行 + 网格行」以便卡片级虚拟化）', () => {
  const groups: DateGroup[] = [
    { date: '2025-01-09', files: [file(1), file(2), file(3), file(4), file(5)] },
    { date: '2025-01-08', files: [file(6)] },
  ];

  it('每组先一行头、再按列数切多行卡片', () => {
    const { rows } = buildTimelineRows(groups, 2);
    // 第一组 5 项 / 2 列 = 1 头 + 3 网格行；第二组 1 项 = 1 头 + 1 网格行
    expect(rows.map((r) => r.type)).toEqual([
      'header',
      'cards',
      'cards',
      'cards',
      'header',
      'cards',
    ]);
    // 头行携带分组，网格行按列切片
    const firstCards = rows[1];
    expect(firstCards.type).toBe('cards');
    if (firstCards.type === 'cards') {
      expect(firstCards.files.map((f) => f.id)).toEqual([1, 2]);
    }
    const lastGridRowOfGroup1 = rows[3];
    if (lastGridRowOfGroup1.type === 'cards') {
      expect(lastGridRowOfGroup1.files.map((f) => f.id)).toEqual([5]);
    }
  });

  it('groupIndexToRow 把分组下标映射到其头行下标，供 scrubber 跳转', () => {
    const { groupIndexToRow } = buildTimelineRows(groups, 2);
    expect(groupIndexToRow).toEqual([0, 4]);
  });

  it('空分组返回空行', () => {
    const { rows, groupIndexToRow } = buildTimelineRows([], 3);
    expect(rows).toEqual([]);
    expect(groupIndexToRow).toEqual([]);
  });

  it('列数至少按 1 处理，避免切片死循环', () => {
    const { rows } = buildTimelineRows([{ date: '2025-01-09', files: [file(1), file(2)] }], 0);
    // 列数兜底为 1：1 头 + 2 网格行
    expect(rows.filter((r) => r.type === 'cards').length).toBe(2);
  });
});
