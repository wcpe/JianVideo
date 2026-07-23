// 回收站「盘符→路径」结构化编辑器的纯函数（FR-87）。
// 前端以行列表编辑，序列化为后端 recycle_bin_paths 的 JSON 串契约，后端不变。

/** 编辑器中的一行：盘符 + 路径 */
export interface RecycleBinRow {
  /** 盘符（如 D），唯一标识一行 */
  drive: string;
  /** 该盘符对应的回收站目录 */
  path: string;
}

/**
 * 把后端存的 JSON 串解析为编辑器行列表。
 * 形如 {"D":"D:/.recycle"} → [{drive:'D', path:'D:/.recycle'}]。
 * 空串、非法 JSON、非对象一律回退为空数组（容错，不抛异常）。
 */
export function parseRecycleBinRows(raw: string): RecycleBinRow[] {
  const text = raw.trim();
  if (!text) return [];
  try {
    const obj = JSON.parse(text) as unknown;
    if (obj === null || typeof obj !== 'object' || Array.isArray(obj)) return [];
    return Object.entries(obj as Record<string, unknown>).map(([drive, path]) => ({
      drive,
      path: typeof path === 'string' ? path : String(path ?? ''),
    }));
  } catch {
    return [];
  }
}

/**
 * 校验行列表是否可提交。
 * 不可提交场景：存在空盘符（去空白后为空）、存在重复盘符。
 * 返回每行的错误信息（无错为 null），便于行内展示。
 */
export interface RecycleBinValidation {
  /** 整体是否有效（无任何行错误） */
  valid: boolean;
  /** 与行列表等长的逐行错误（无错为 null） */
  rowErrors: (string | null)[];
}

export function validateRecycleBinRows(rows: RecycleBinRow[]): RecycleBinValidation {
  // 统计每个去空白盘符的出现次数，用于重复检测
  const counts = new Map<string, number>();
  for (const row of rows) {
    const key = row.drive.trim();
    if (!key) continue;
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }

  const rowErrors = rows.map((row) => {
    const key = row.drive.trim();
    if (!key) return '盘符不能为空';
    if ((counts.get(key) ?? 0) > 1) return '盘符重复';
    return null;
  });

  return { valid: rowErrors.every((e) => e === null), rowErrors };
}

/**
 * 把行列表序列化为后端 recycle_bin_paths 的 JSON 串。
 * 仅纳入盘符非空的行；盘符去空白后作为键；空列表序列化为 "{}"。
 * 调用前应先 validateRecycleBinRows 确认可提交。
 */
export function serializeRecycleBinRows(rows: RecycleBinRow[]): string {
  const obj: Record<string, string> = {};
  for (const row of rows) {
    const key = row.drive.trim();
    if (!key) continue;
    obj[key] = row.path;
  }
  return JSON.stringify(obj);
}

/** 回收站到期提示（FR2-054）：由 deleted_at + 保留天数计算预计自动清理时间 */
export type RecycleExpiryHint =
  | { kind: 'disabled'; text: string }
  | { kind: 'expired'; text: string; expiresAt: Date }
  | { kind: 'pending'; text: string; expiresAt: Date };

/**
 * 计算回收站项的到期提示文案。
 * - autoCleanupEnabled=false 或 retentionDays<=0：仅手动清理
 * - deleted_at 无效：返回 null（调用方可不展示副标题）
 * - 已过期：提示「已到期，等待自动清理」
 * - 未过期：提示「预计 YYYY-MM-DD 自动清理」
 */
export function formatRecycleExpiryHint(
  deletedAt: string | null | undefined,
  retentionDays: number,
  autoCleanupEnabled: boolean,
  now: Date = new Date(),
): RecycleExpiryHint | null {
  if (!autoCleanupEnabled || !Number.isFinite(retentionDays) || retentionDays <= 0) {
    return { kind: 'disabled', text: '自动清理已关闭，仅可手动清理' };
  }
  if (!deletedAt) return null;
  const deleted = new Date(deletedAt);
  if (Number.isNaN(deleted.getTime())) return null;

  const expiresAt = new Date(deleted.getTime() + retentionDays * 24 * 60 * 60 * 1000);
  const dateText = formatLocalDate(expiresAt);
  if (expiresAt.getTime() <= now.getTime()) {
    return { kind: 'expired', text: `已到期（${dateText}），等待自动清理`, expiresAt };
  }
  return { kind: 'pending', text: `预计 ${dateText} 自动清理`, expiresAt };
}

/** 本地日期 YYYY-MM-DD（避免 toISOString 时区偏移） */
function formatLocalDate(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}
