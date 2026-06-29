import { describe, it, expect } from 'vitest';
import { strToDate, dateToStr } from './MediaQueryFilters.helpers';

// FR-94：原生日期控件换 Mantine DatePickerInput 后，YYYY-MM-DD ↔ 本地 Date 的转换契约，
// 保证请求侧仍传 YYYY-MM-DD 字符串、且不因 UTC 解析/格式化差一天。
describe('日期范围 字符串↔Date 转换（FR-94）', () => {
  it('空串解析为 null、null 格式化为空串（清除筛选不带参）', () => {
    expect(strToDate('')).toBeNull();
    expect(dateToStr(null)).toBe('');
  });

  it('YYYY-MM-DD 往返不变（本地年月日，不差一天）', () => {
    const d = strToDate('2026-06-24');
    expect(d).toBeInstanceOf(Date);
    expect(d?.getFullYear()).toBe(2026);
    expect(d?.getMonth()).toBe(5); // 6 月 → 下标 5
    expect(d?.getDate()).toBe(24);
    expect(dateToStr(d)).toBe('2026-06-24');
  });

  it('个位月/日补零', () => {
    expect(dateToStr(strToDate('2026-01-05'))).toBe('2026-01-05');
  });

  it('非法输入安全降级为空串', () => {
    expect(strToDate('not-a-date')).toBeNull();
    expect(dateToStr(new Date('invalid'))).toBe('');
  });

  it('兼容字符串型日期值（防 DatePicker 版本差异）', () => {
    expect(dateToStr('2026-06-24')).toBe('2026-06-24');
  });
});
