import type { UpdateCheckResult } from '@/types';

// 更新检查结果的本地缓存键前缀；按频道分别缓存（正式版/候选版 RC 各一份）。
// 缓存用途：进入「应用更新」即时展示上次的版本信息与发布说明（含更新日志），免手动点检查。
const KEY_PREFIX = 'jianvideo.update.check.';
const STRICT_RC_TAG_PATTERN = /^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)-rc\.[1-9]\d*$/;

// loadCachedUpdate 读取某频道上次检查结果的本地缓存；缺失或损坏一律返回 null（安全兜底）。
export function loadCachedUpdate(channel: string): UpdateCheckResult | null {
  try {
    const raw = localStorage.getItem(KEY_PREFIX + channel);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    // 基本形状校验：仅接受含字符串 current 字段的对象，避免旧/坏数据污染渲染
    if (!parsed || typeof parsed !== 'object' || typeof parsed.current !== 'string') return null;
    if (
      channel === 'prerelease' &&
      (parsed.prerelease !== true ||
        typeof parsed.tag !== 'string' ||
        !STRICT_RC_TAG_PATTERN.test(parsed.tag) ||
        typeof parsed.latest !== 'string' ||
        !STRICT_RC_TAG_PATTERN.test(parsed.latest))
    ) {
      // 旧 dev 或非法 RC 缓存不再属于候选频道，迁移时直接失效。
      localStorage.removeItem(KEY_PREFIX + channel);
      return null;
    }
    return parsed as UpdateCheckResult;
  } catch {
    // 解析失败 / localStorage 不可用（隐私模式）→ 视为无缓存
    return null;
  }
}

// saveCachedUpdate 持久化某频道的检查结果（含发布说明），供下次进入即时展示。
export function saveCachedUpdate(channel: string, info: UpdateCheckResult): void {
  try {
    localStorage.setItem(KEY_PREFIX + channel, JSON.stringify(info));
  } catch {
    // localStorage 不可用（配额/隐私模式）时静默，不影响功能
  }
}

// clearCachedUpdate 清除某频道缓存（FR-112）：执行更新成功后调用，使下次进入重新强制拉取最新。
export function clearCachedUpdate(channel: string): void {
  try {
    localStorage.removeItem(KEY_PREFIX + channel);
  } catch {
    // localStorage 不可用时静默，不影响功能
  }
}
