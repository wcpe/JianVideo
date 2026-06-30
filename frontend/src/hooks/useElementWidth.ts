import { useState, useEffect, type RefObject } from 'react';

/**
 * 观测元素当前内容宽度（FR-141）：用于按真实容器宽度推导网格列数与缩略图尺寸。
 * - 挂载即测一次，并通过 ResizeObserver 跟随后续尺寸变化（节流交由浏览器）。
 * - 无 ResizeObserver 的环境（如测试 jsdom）降级为初始测量值，不抛错。
 * - 返回当前宽度（像素，整数），初始为 0 直至首次测量完成。
 */
export function useElementWidth(ref: RefObject<HTMLElement | null>): number {
  const [width, setWidth] = useState(0);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    // 首次同步测量，避免首帧用 0 选错列数/缩略图档位
    setWidth(el.getBoundingClientRect().width);
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) setWidth(entry.contentRect.width);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);

  return Math.round(width);
}
