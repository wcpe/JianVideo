// VideoPlayer 的音量 / 静音偏好纯逻辑与类型（FR-104）：从组件文件拆出，
// 使 VideoPlayer.tsx 仅导出组件，满足 react-refresh/only-export-components。

// 音量 / 静音偏好持久化键（localStorage，FR-104）
export const VOLUME_PREF_KEY = 'jianvideo.player.volume';

/** 音量 / 静音偏好（FR-104）。 */
export interface VolumePref {
  /** 音量 [0,1] */
  volume: number;
  /** 是否静音 */
  muted: boolean;
}

/**
 * 读取持久化的音量 / 静音偏好（纯函数，FR-104）。
 * 无存储 / 内容损坏 / 字段非法时返回 null（不抛），volume 夹取到 [0,1]。
 */
export function loadVolumePref(): VolumePref | null {
  try {
    const raw = localStorage.getItem(VOLUME_PREF_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { volume?: unknown; muted?: unknown };
    if (typeof parsed.volume !== 'number' || isNaN(parsed.volume)) return null;
    const volume = Math.min(1, Math.max(0, parsed.volume));
    return { volume, muted: Boolean(parsed.muted) };
  } catch {
    return null;
  }
}

/** 把音量夹取到 [0,1]（纯函数，FR-104）。非有限数归 0，防止越界值赋给 video.volume 触发 IndexSizeError。 */
export function clampVolume(val: number): number {
  if (!Number.isFinite(val)) return 0;
  return Math.min(1, Math.max(0, val));
}

/** 写入音量 / 静音偏好（纯函数，FR-104）。失败静默忽略（如隐私模式禁写）。 */
export function saveVolumePref(pref: VolumePref): void {
  try {
    const volume = Math.min(1, Math.max(0, pref.volume));
    localStorage.setItem(VOLUME_PREF_KEY, JSON.stringify({ volume, muted: Boolean(pref.muted) }));
  } catch {
    /* 静默：localStorage 不可用时不影响播放 */
  }
}
