import { describe, it, expect } from 'vitest';
import {
  groupMediaByDate,
  positionToGroupIndex,
  normalizeDateQuery,
  resolveDateToGroupIndex,
  groupDateAtIndex,
  summarizeGroup,
} from './timeline';
import type { DateGroup } from './timeline';
import type { MediaFile } from '@/types';

/** 构造测试用媒体文件，仅关心 id 与 added_at */
function makeFile(id: number, addedAt: string): MediaFile {
  return {
    id,
    library_id: 1,
    file_path: `D:\\Videos\\${id}.mp4`,
    file_name: `${id}.mp4`,
    file_size: 0,
    format: 'mp4',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 0,
    height: 0,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: addedAt,
    modified_at: addedAt,
  };
}

describe('groupMediaByDate', () => {
  it('按日期分组并倒序排列', () => {
    const groups = groupMediaByDate([
      makeFile(1, '2025-01-09T12:00:00Z'),
      makeFile(2, '2025-01-01T08:00:00Z'),
      makeFile(3, '2025-03-15T20:00:00Z'),
    ]);
    expect(groups.map((g) => g.date)).toEqual(['2025-03-15', '2025-01-09', '2025-01-01']);
  });

  it('同一日期的文件合并到同一组并保持输入顺序', () => {
    const groups = groupMediaByDate([
      makeFile(1, '2025-01-09T01:00:00Z'),
      makeFile(2, '2025-01-09T23:00:00Z'),
      makeFile(3, '2025-01-09T12:00:00Z'),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].date).toBe('2025-01-09');
    expect(groups[0].files.map((f) => f.id)).toEqual([1, 2, 3]);
  });

  it('空字符串与非法 added_at 归入“未知日期”组', () => {
    const groups = groupMediaByDate([
      makeFile(1, ''),
      makeFile(2, 'not-a-date'),
      makeFile(3, '2025'),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].date).toBe('未知日期');
    expect(groups[0].files.map((f) => f.id)).toEqual([1, 2, 3]);
  });

  it('“未知日期”组始终排在所有有效日期之后', () => {
    const groups = groupMediaByDate([
      makeFile(1, ''),
      makeFile(2, '2025-01-01T00:00:00Z'),
      makeFile(3, '2024-12-31T00:00:00Z'),
    ]);
    expect(groups.map((g) => g.date)).toEqual(['2025-01-01', '2024-12-31', '未知日期']);
  });

  it('空输入返回空数组', () => {
    expect(groupMediaByDate([])).toEqual([]);
  });

  // FR-32：缩放粒度与媒体时间组织
  it('media_time 优先于 added_at 作为时间源', () => {
    const f = makeFile(1, '2020-01-01T00:00:00Z');
    f.media_time = '2023-08-15T10:00:00Z';
    const groups = groupMediaByDate([f]);
    expect(groups[0].date).toBe('2023-08-15');
  });

  it('按月粒度分组（YYYY-MM）', () => {
    const groups = groupMediaByDate(
      [
        makeFile(1, '2025-01-09T12:00:00Z'),
        makeFile(2, '2025-01-20T12:00:00Z'),
        makeFile(3, '2024-12-31T12:00:00Z'),
      ],
      'month',
    );
    expect(groups.map((g) => g.date)).toEqual(['2025-01', '2024-12']);
    expect(groups[0].files.map((x) => x.id)).toEqual([1, 2]);
  });

  it('按年粒度分组（YYYY）', () => {
    const groups = groupMediaByDate(
      [
        makeFile(1, '2025-01-09T12:00:00Z'),
        makeFile(2, '2024-06-01T12:00:00Z'),
        makeFile(3, '2024-12-31T12:00:00Z'),
      ],
      'year',
    );
    expect(groups.map((g) => g.date)).toEqual(['2025', '2024']);
    expect(groups[1].files.map((x) => x.id)).toEqual([2, 3]);
  });

  // FR-68 缩放健壮性：媒体时间降级链 media_time → added_at → modified_at
  it('media_time 与 added_at 均无效时回退 modified_at（不归未知日期）', () => {
    const f = makeFile(1, 'not-a-date'); // added_at 非法
    f.media_time = ''; // 空字符串视为缺失
    f.modified_at = '2022-05-06T03:00:00Z'; // 有效的修改时间
    const groups = groupMediaByDate([f]);
    expect(groups[0].date).toBe('2022-05-06');
  });

  it('三个时间源全部缺失/非法才归入“未知日期”', () => {
    const f = makeFile(1, '');
    f.media_time = null;
    f.modified_at = 'bad';
    const groups = groupMediaByDate([f]);
    expect(groups[0].date).toBe('未知日期');
  });

  // FR-120：“所有”粒度——不按日期分组，全部并入单组、保持输入顺序
  it('“所有”粒度不分组：全部并入单组并保持输入顺序', () => {
    const groups = groupMediaByDate(
      [
        makeFile(1, '2025-03-15T20:00:00Z'),
        makeFile(2, '2025-01-01T08:00:00Z'),
        makeFile(3, '2024-12-31T00:00:00Z'),
      ],
      'all',
    );
    expect(groups).toHaveLength(1);
    expect(groups[0].files.map((f) => f.id)).toEqual([1, 2, 3]);
  });

  it('“所有”粒度空输入返回空数组', () => {
    expect(groupMediaByDate([], 'all')).toEqual([]);
  });
});

// FR-68 scrubber 位置 → 目标分组映射
describe('positionToGroupIndex', () => {
  it('顶部比例 0 映射到第一组（最新）', () => {
    expect(positionToGroupIndex(0, 5)).toBe(0);
  });

  it('底部比例 1 映射到最后一组（最旧）', () => {
    expect(positionToGroupIndex(1, 5)).toBe(4);
  });

  it('中间比例落在对应均分段', () => {
    // 5 组均分 [0,1) → 每段 0.2：0.5 落第 3 段（下标 2）
    expect(positionToGroupIndex(0.5, 5)).toBe(2);
    // 0.25 落第 2 段（下标 1）
    expect(positionToGroupIndex(0.25, 5)).toBe(1);
    // 0.85 落第 5 段（下标 4）
    expect(positionToGroupIndex(0.85, 5)).toBe(4);
  });

  it('越界比例钳制到 [0, count-1]', () => {
    expect(positionToGroupIndex(-0.3, 5)).toBe(0);
    expect(positionToGroupIndex(1.7, 5)).toBe(4);
  });

  it('单组始终返回 0', () => {
    expect(positionToGroupIndex(0, 1)).toBe(0);
    expect(positionToGroupIndex(0.9, 1)).toBe(0);
    expect(positionToGroupIndex(1, 1)).toBe(0);
  });

  it('空组（count<=0）返回 0', () => {
    expect(positionToGroupIndex(0.5, 0)).toBe(0);
    expect(positionToGroupIndex(0.5, -2)).toBe(0);
  });
});

// FR-142：日期查询规整
describe('normalizeDateQuery', () => {
  it('年 YYYY 补全到当年最后一天', () => {
    expect(normalizeDateQuery('2025')).toBe('2025-12-31');
  });

  it('月 YYYY-MM 补全到当月最后一天（含 2 月闰年判定）', () => {
    expect(normalizeDateQuery('2025-01')).toBe('2025-01-31');
    expect(normalizeDateQuery('2025-02')).toBe('2025-02-28'); // 平年
    expect(normalizeDateQuery('2024-02')).toBe('2024-02-29'); // 闰年
    expect(normalizeDateQuery('2025-04')).toBe('2025-04-30');
  });

  it('日 YYYY-MM-DD 原样补零规整', () => {
    expect(normalizeDateQuery('2025-03-15')).toBe('2025-03-15');
    expect(normalizeDateQuery('2025-3-5')).toBe('2025-03-05');
  });

  it('接受 / 分隔并规整为 -', () => {
    expect(normalizeDateQuery('2025/03')).toBe('2025-03-31');
    expect(normalizeDateQuery('2025/03/15')).toBe('2025-03-15');
  });

  it('去除首尾空白后解析', () => {
    expect(normalizeDateQuery('  2025-03  ')).toBe('2025-03-31');
  });

  it('非法输入返回空串', () => {
    expect(normalizeDateQuery('')).toBe('');
    expect(normalizeDateQuery('abc')).toBe('');
    expect(normalizeDateQuery('2025-13')).toBe(''); // 月越界
    expect(normalizeDateQuery('2025-00')).toBe(''); // 月越界
    expect(normalizeDateQuery('2025-03-40')).toBe(''); // 日越界
    expect(normalizeDateQuery('25-03')).toBe(''); // 年位数不足
  });
});

// FR-142：日期查询 → 目标分组下标
describe('resolveDateToGroupIndex', () => {
  // 分组已倒序：顶部=最新
  const dayGroups: DateGroup[] = [
    { date: '2025-03-15', files: [] },
    { date: '2025-01-09', files: [] },
    { date: '2024-12-31', files: [] },
    { date: '2024-06-01', files: [] },
  ];

  it('精确命中某日分组', () => {
    expect(resolveDateToGroupIndex(dayGroups, '2025-01-09')).toBe(1);
  });

  it('查询落在两组之间时取不晚于查询日的最新一组', () => {
    // 2025-02 末（2025-02-28）≤ 之的最新分组是 2025-01-09（下标 1）
    expect(resolveDateToGroupIndex(dayGroups, '2025-02')).toBe(1);
  });

  it('按年查询落到该年内最新分组', () => {
    // 2024 末（2024-12-31）→ 命中 2024-12-31（下标 2）
    expect(resolveDateToGroupIndex(dayGroups, '2024')).toBe(2);
  });

  it('查询晚于全部数据落到最新分组（下标 0）', () => {
    expect(resolveDateToGroupIndex(dayGroups, '2030')).toBe(0);
  });

  it('查询早于全部数据落到最旧的有效分组（末位）', () => {
    expect(resolveDateToGroupIndex(dayGroups, '2000')).toBe(3);
  });

  it('跳过「未知日期」非法分组', () => {
    const withUnknown: DateGroup[] = [
      { date: '2025-01-09', files: [] },
      { date: '未知日期', files: [] },
    ];
    // 查询早于全部有效分组 → 落到最旧的有效分组（下标 0，未知日期被跳过）
    expect(resolveDateToGroupIndex(withUnknown, '2000')).toBe(0);
  });

  it('与月粒度分组键同口径比较', () => {
    const monthGroups: DateGroup[] = [
      { date: '2025-03', files: [] },
      { date: '2025-01', files: [] },
      { date: '2024-12', files: [] },
    ];
    expect(resolveDateToGroupIndex(monthGroups, '2025-03-15')).toBe(0);
    expect(resolveDateToGroupIndex(monthGroups, '2025-02')).toBe(1);
    expect(resolveDateToGroupIndex(monthGroups, '2024')).toBe(2);
  });

  it('查询非法返回 -1', () => {
    expect(resolveDateToGroupIndex(dayGroups, 'abc')).toBe(-1);
    expect(resolveDateToGroupIndex(dayGroups, '')).toBe(-1);
  });

  it('空分组返回 -1', () => {
    expect(resolveDateToGroupIndex([], '2025')).toBe(-1);
  });
});

// FR-142：取指定下标分组日期（视口锁定用）
describe('groupDateAtIndex', () => {
  const groups: DateGroup[] = [
    { date: '2025-03-15', files: [] },
    { date: '2025-01-09', files: [] },
  ];

  it('返回给定下标分组的日期键', () => {
    expect(groupDateAtIndex(groups, 0)).toBe('2025-03-15');
    expect(groupDateAtIndex(groups, 1)).toBe('2025-01-09');
  });

  it('越界返回空串', () => {
    expect(groupDateAtIndex(groups, -1)).toBe('');
    expect(groupDateAtIndex(groups, 2)).toBe('');
    expect(groupDateAtIndex([], 0)).toBe('');
  });
});

describe('summarizeGroup（FR-146 分组头聚合）', () => {
  /** 构造带相机/地点的媒体文件 */
  function makeMeta(id: number, camera?: string, location?: string): MediaFile {
    return { ...makeFile(id, '2025-01-01T00:00:00Z'), camera, location };
  }

  it('数量取组内条数', () => {
    const group: DateGroup = {
      date: '2025-01-01',
      files: [makeMeta(1), makeMeta(2), makeMeta(3)],
    };
    expect(summarizeGroup(group).count).toBe(3);
  });

  it('设备取组内最常见相机', () => {
    const group: DateGroup = {
      date: '2025-01-01',
      files: [
        makeMeta(1, 'Canon EOS R5'),
        makeMeta(2, 'iPhone 15'),
        makeMeta(3, 'iPhone 15'),
      ],
    };
    expect(summarizeGroup(group).camera).toBe('iPhone 15');
  });

  it('地点取组内最常见地名', () => {
    const group: DateGroup = {
      date: '2025-01-01',
      files: [
        makeMeta(1, undefined, '浙江·杭州'),
        makeMeta(2, undefined, '北京·北京'),
        makeMeta(3, undefined, '浙江·杭州'),
      ],
    };
    expect(summarizeGroup(group).location).toBe('浙江·杭州');
  });

  it('无相机/地点时对应维度为空串', () => {
    const group: DateGroup = { date: '2025-01-01', files: [makeMeta(1), makeMeta(2)] };
    const s = summarizeGroup(group);
    expect(s.camera).toBe('');
    expect(s.location).toBe('');
  });

  it('忽略空白值，并列取先出现者', () => {
    const group: DateGroup = {
      date: '2025-01-01',
      files: [
        makeMeta(1, '  ', ''),
        makeMeta(2, 'A', '甲'),
        makeMeta(3, 'B', '乙'),
      ],
    };
    // A 与 B 各 1 次并列，取先出现的 A；地点同理取先出现的甲
    const s = summarizeGroup(group);
    expect(s.camera).toBe('A');
    expect(s.location).toBe('甲');
  });
});
