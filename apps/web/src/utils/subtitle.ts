import type { SubtitleEntry } from '@/types';

/**
 * 轻量 WebVTT 解析器。
 * 将 WebVTT 文本解析为按时间排序的字幕条目数组。
 */
export function parseWebVTT(vttText: string): SubtitleEntry[] {
  const entries: SubtitleEntry[] = [];
  const blocks = vttText.split(/\n\n+/);

  for (const block of blocks) {
    const lines = block.trim().split('\n');
    const timingIdx = lines.findIndex((l) => l.includes('-->'));
    if (timingIdx < 0) continue;

    const timeLine = lines[timingIdx];
    const parts = timeLine.split('-->');
    if (parts.length !== 2) continue;

    const start = parseVTTTime(parts[0].trim());
    const end = parseVTTTime(parts[1].trim());
    if (start < 0 || end < 0) continue;

    const text = lines
      .slice(timingIdx + 1)
      .join('\n')
      .trim();
    if (text) entries.push({ start, end, text });
  }

  return entries;
}

/**
 * 解析 WebVTT 时间戳 (HH:MM:SS.mmm 或 MM:SS.mmm) 为秒数。
 */
export function parseVTTTime(ts: string): number {
  const parts = ts.split(':').map((s) => parseFloat(s.trim()));
  if (parts.some(isNaN)) return -1;
  if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2];
  if (parts.length === 2) return parts[0] * 60 + parts[1];
  return -1;
}
