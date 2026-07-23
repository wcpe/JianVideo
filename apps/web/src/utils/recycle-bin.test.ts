import { describe, it, expect } from 'vitest';
import {
  parseRecycleBinRows,
  validateRecycleBinRows,
  serializeRecycleBinRows,
  formatRecycleExpiryHint,
} from './recycle-bin';

describe('parseRecycleBinRows', () => {
  it('把既有 JSON 解析为对应行', () => {
    expect(parseRecycleBinRows('{"D":"D:/.recycle"}')).toEqual([
      { drive: 'D', path: 'D:/.recycle' },
    ]);
  });

  it('多盘符按对象顺序解析为多行', () => {
    expect(parseRecycleBinRows('{"C":"C:/.r","D":"D:/.r"}')).toEqual([
      { drive: 'C', path: 'C:/.r' },
      { drive: 'D', path: 'D:/.r' },
    ]);
  });

  it('空串回退空数组', () => {
    expect(parseRecycleBinRows('')).toEqual([]);
    expect(parseRecycleBinRows('   ')).toEqual([]);
  });

  it('非法 JSON 回退空数组', () => {
    expect(parseRecycleBinRows('{not json')).toEqual([]);
  });

  it('非对象（数组/标量）回退空数组', () => {
    expect(parseRecycleBinRows('[1,2]')).toEqual([]);
    expect(parseRecycleBinRows('"x"')).toEqual([]);
    expect(parseRecycleBinRows('null')).toEqual([]);
  });
});

describe('validateRecycleBinRows', () => {
  it('全部合法时 valid 为真、无行错', () => {
    const res = validateRecycleBinRows([
      { drive: 'C', path: 'C:/.r' },
      { drive: 'D', path: 'D:/.r' },
    ]);
    expect(res.valid).toBe(true);
    expect(res.rowErrors).toEqual([null, null]);
  });

  it('空盘符标错且整体不可提交', () => {
    const res = validateRecycleBinRows([{ drive: '  ', path: 'X:/.r' }]);
    expect(res.valid).toBe(false);
    expect(res.rowErrors[0]).toMatch(/盘符不能为空/);
  });

  it('重复盘符两行均标错且整体不可提交', () => {
    const res = validateRecycleBinRows([
      { drive: 'D', path: 'D:/a' },
      { drive: 'D', path: 'D:/b' },
    ]);
    expect(res.valid).toBe(false);
    expect(res.rowErrors[0]).toMatch(/盘符重复/);
    expect(res.rowErrors[1]).toMatch(/盘符重复/);
  });
});

describe('serializeRecycleBinRows', () => {
  it('行列表序列化为 JSON 串', () => {
    expect(serializeRecycleBinRows([{ drive: 'D', path: 'D:/.recycle' }])).toBe(
      '{"D":"D:/.recycle"}',
    );
  });

  it('盘符去空白后作键', () => {
    expect(serializeRecycleBinRows([{ drive: ' D ', path: 'D:/.r' }])).toBe('{"D":"D:/.r"}');
  });

  it('跳过空盘符行', () => {
    expect(
      serializeRecycleBinRows([
        { drive: '', path: 'x' },
        { drive: 'E', path: 'E:/.r' },
      ]),
    ).toBe('{"E":"E:/.r"}');
  });

  it('空列表序列化为空对象串', () => {
    expect(serializeRecycleBinRows([])).toBe('{}');
  });
});

describe('formatRecycleExpiryHint（FR2-054）', () => {
  const now = new Date('2026-07-23T12:00:00');

  it('开关关闭时提示仅可手动清理', () => {
    const hint = formatRecycleExpiryHint('2026-07-01T00:00:00Z', 30, false, now);
    expect(hint?.kind).toBe('disabled');
    expect(hint?.text).toMatch(/仅可手动清理/);
  });

  it('保留天数为 0 时提示仅可手动清理', () => {
    const hint = formatRecycleExpiryHint('2026-07-01T00:00:00Z', 0, true, now);
    expect(hint?.kind).toBe('disabled');
  });

  it('deleted_at 缺失返回 null', () => {
    expect(formatRecycleExpiryHint(null, 30, true, now)).toBeNull();
    expect(formatRecycleExpiryHint(undefined, 30, true, now)).toBeNull();
  });

  it('未到期时展示预计清理日期', () => {
    // 2026-07-10 + 30 天 = 2026-08-09
    const hint = formatRecycleExpiryHint('2026-07-10T00:00:00', 30, true, now);
    expect(hint?.kind).toBe('pending');
    expect(hint?.text).toMatch(/预计 2026-08-09 自动清理/);
  });

  it('已到期时展示等待自动清理', () => {
    // 2026-06-01 + 30 天 = 2026-07-01，相对 now 已过期
    const hint = formatRecycleExpiryHint('2026-06-01T00:00:00', 30, true, now);
    expect(hint?.kind).toBe('expired');
    expect(hint?.text).toMatch(/已到期/);
    expect(hint?.text).toMatch(/等待自动清理/);
  });
});
