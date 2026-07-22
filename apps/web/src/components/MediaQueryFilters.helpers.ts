// MediaQueryFilters 的日期互转纯逻辑（FR-36）：从组件文件拆出，
// 使 MediaQueryFilters.tsx 仅导出组件，满足 react-refresh/only-export-components。

// YYYY-MM-DD 字符串 ↔ 本地 Date：用本地年月日构造/格式化，避免 UTC 解析或 toISOString 导致差一天。
// 请求侧契约不变（仍传 YYYY-MM-DD 字符串给后端）。
export function strToDate(s: string): Date | null {
  if (!s) return null;
  const [y, m, d] = s.split('-').map(Number);
  if (!y || !m || !d) return null;
  return new Date(y, m - 1, d);
}
export function dateToStr(v: Date | string | null): string {
  if (!v) return '';
  const d = typeof v === 'string' ? new Date(v) : v;
  if (isNaN(d.getTime())) return '';
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
