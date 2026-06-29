import { useState, useCallback } from 'react';

// 桌面端侧边导航展开态宽度的 localStorage 键（像素值字符串）
const STORAGE_KEY = 'jianvideo.nav.width';
// 展开态宽度夹紧范围与默认值（像素）：默认沿用原固定展开宽度，min/max 限制可拖拽范围
export const NAV_WIDTH_MIN = 160;
export const NAV_WIDTH_MAX = 360;
export const NAV_WIDTH_DEFAULT = 180;

// 把任意数值夹紧到 [NAV_WIDTH_MIN, NAV_WIDTH_MAX]；非有限数回退默认值，便于穷举单测
export function clampNavWidth(width: number): number {
  if (!Number.isFinite(width)) return NAV_WIDTH_DEFAULT;
  if (width < NAV_WIDTH_MIN) return NAV_WIDTH_MIN;
  if (width > NAV_WIDTH_MAX) return NAV_WIDTH_MAX;
  return Math.round(width);
}

// 读取持久化的展开态宽度，缺失/非法/读取异常时回退默认值（已夹紧）
function readWidth(): number {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === null) return NAV_WIDTH_DEFAULT;
    return clampNavWidth(Number(raw));
  } catch {
    // localStorage 不可用（如隐私模式）时降级为默认宽度，不影响导航可用性
    return NAV_WIDTH_DEFAULT;
  }
}

// 管理桌面端侧边导航展开态宽度并持久化到 localStorage，刷新后保持。
// 返回 [width, setWidth]：width 为当前夹紧后的展开宽度，setWidth 设置新宽度（自动夹紧并持久化）。
// 仅作用于展开态；收缩态固定图标宽度、移动端抽屉均不消费本 hook。
export function useNavWidth(): [number, (width: number) => void] {
  const [width, setWidthState] = useState<number>(readWidth);

  const setWidth = useCallback((next: number) => {
    const clamped = clampNavWidth(next);
    setWidthState(clamped);
    try {
      localStorage.setItem(STORAGE_KEY, String(clamped));
    } catch {
      // 持久化失败不阻断调整，仅本次会话内生效
    }
  }, []);

  return [width, setWidth];
}
