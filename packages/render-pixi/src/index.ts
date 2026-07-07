export interface VisibleWindow {
  readonly start: number;
  readonly end: number;
}

export function resolveVisibleWindow(total: number, firstVisible: number, visibleCount: number, overscan: number): VisibleWindow {
  const start = Math.max(0, firstVisible - overscan);
  const end = Math.min(total, firstVisible + visibleCount + overscan);
  return { start, end };
}

export function estimateTextureMemoryBytes(textureCount: number, width: number, height: number): number {
  return textureCount * width * height * 4;
}
