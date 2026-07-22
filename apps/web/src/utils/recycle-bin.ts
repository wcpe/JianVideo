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
