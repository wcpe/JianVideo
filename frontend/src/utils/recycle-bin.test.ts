import { describe, it, expect } from 'vitest';
import {
  parseRecycleBinRows,
  validateRecycleBinRows,
  serializeRecycleBinRows,
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
