const STORAGE_KEY = 'jianvideo.subtitle.preferences';
export const FONT_SIZES = ['small', 'medium', 'large'] as const;
export const SUBTITLE_COLORS = ['#ffffff', '#ffff00', '#00ffff'] as const;
export const BACKGROUND_OPACITIES = [0, 0.4, 0.7] as const;
export const VERTICAL_POSITIONS = [8, 16, 24] as const;

export interface SubtitlePreferences {
  readonly fontSize: (typeof FONT_SIZES)[number];
  readonly color: (typeof SUBTITLE_COLORS)[number];
  readonly backgroundOpacity: (typeof BACKGROUND_OPACITIES)[number];
  readonly verticalPosition: (typeof VERTICAL_POSITIONS)[number];
}

export const DEFAULT_SUBTITLE_PREFERENCES: SubtitlePreferences = {
  fontSize: 'medium',
  color: '#ffffff',
  backgroundOpacity: 0.4,
  verticalPosition: 8,
};

export function loadSubtitlePreferences(): SubtitlePreferences {
  if (typeof localStorage === 'undefined') return DEFAULT_SUBTITLE_PREFERENCES;
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null');
    return isSubtitlePreferences(parsed) ? parsed : DEFAULT_SUBTITLE_PREFERENCES;
  } catch {
    return DEFAULT_SUBTITLE_PREFERENCES;
  }
}

export function saveSubtitlePreferences(preferences: SubtitlePreferences): void {
  if (typeof localStorage === 'undefined') return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(preferences));
  } catch {
    // 浏览器拒绝持久化时保留当前会话内已应用的字幕样式。
  }
}

function isSubtitlePreferences(value: unknown): value is SubtitlePreferences {
  if (!value || typeof value !== 'object') return false;
  const candidate = value as Partial<SubtitlePreferences>;
  return (
    FONT_SIZES.includes(candidate.fontSize as SubtitlePreferences['fontSize']) &&
    SUBTITLE_COLORS.includes(candidate.color as SubtitlePreferences['color']) &&
    BACKGROUND_OPACITIES.includes(
      candidate.backgroundOpacity as SubtitlePreferences['backgroundOpacity'],
    ) &&
    VERTICAL_POSITIONS.includes(
      candidate.verticalPosition as SubtitlePreferences['verticalPosition'],
    )
  );
}
