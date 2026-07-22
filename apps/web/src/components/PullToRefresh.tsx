import { useRef, useState, useCallback, type ReactNode } from 'react';
import { Box, Center, Loader, Text } from '@mantine/core';

interface PullToRefreshProps {
  // 松手触发刷新（达阈值时调用），返回 Promise 以便刷新中展示加载态
  onRefresh: () => Promise<void> | void;
  // 是否启用（仅窄屏移动端启用；桌面传 false 完全不绑定手势）
  enabled: boolean;
  children: ReactNode;
}

// 触发刷新的下拉位移阈值（像素）
const THRESHOLD = 70;
// 下拉位移到指示器位移的阻尼系数（越小越「拖不动」，模拟橡皮筋）
const DAMPING = 0.5;
// 指示器最大下拉位移（像素），避免拖出过长
const MAX_PULL = 100;

/**
 * 下拉刷新容器（FR-143）：仅窄屏移动端启用。
 * - 仅当页面已滚动到顶部（window.scrollY<=0）且向下拖动时累积位移；
 *   松手位移≥阈值则触发 onRefresh，否则回弹不触发。
 * - 向上拖动、非顶部、刷新进行中均不累积。位移经阻尼系数，模拟橡皮筋手感。
 * - disabled（桌面）时手势处理短路，等同纯透传容器。
 */
export default function PullToRefresh({ onRefresh, enabled, children }: PullToRefreshProps) {
  // 当前下拉位移（指示器与内容下移量，像素）
  const [pull, setPull] = useState(0);
  // 刷新进行中：展示 Loader 并阻止重复触发
  const [refreshing, setRefreshing] = useState(false);
  // 起手 Y：null 表示本次手势不在顶部起手、不参与下拉
  const startY = useRef<number | null>(null);

  const handleTouchStart = useCallback(
    (e: React.TouchEvent) => {
      // 仅在启用、未刷新、页面处于顶部时记录起手点
      if (!enabled || refreshing || window.scrollY > 0) {
        startY.current = null;
        return;
      }
      startY.current = e.touches[0].clientY;
    },
    [enabled, refreshing],
  );

  const handleTouchMove = useCallback((e: React.TouchEvent) => {
    if (startY.current === null) return;
    const delta = e.touches[0].clientY - startY.current;
    // 仅向下拖动累积；向上拖动视为正常滚动，重置位移
    if (delta <= 0) {
      setPull(0);
      return;
    }
    setPull(Math.min(MAX_PULL, delta * DAMPING));
  }, []);

  const handleTouchEnd = useCallback(
    async (e: React.TouchEvent) => {
      if (startY.current === null) return;
      const delta = e.changedTouches[0].clientY - startY.current;
      startY.current = null;
      // 达阈值即触发刷新；阈值以原始位移（未阻尼）计算更直观
      if (delta >= THRESHOLD && !refreshing) {
        setRefreshing(true);
        setPull(THRESHOLD * DAMPING);
        try {
          await onRefresh();
        } finally {
          setRefreshing(false);
          setPull(0);
        }
      } else {
        setPull(0);
      }
    },
    [onRefresh, refreshing],
  );

  return (
    <Box
      data-testid="pull-to-refresh"
      onTouchStart={handleTouchStart}
      onTouchMove={handleTouchMove}
      onTouchEnd={handleTouchEnd}
      style={{ position: 'relative' }}
    >
      {/* 下拉指示器：随位移浮现，刷新中转圈，达阈值前提示「下拉刷新」 */}
      {(pull > 0 || refreshing) && (
        <Center
          aria-hidden={!refreshing}
          style={{ height: pull, overflow: 'hidden', transition: 'height 0.2s' }}
        >
          {refreshing ? (
            <Loader size="sm" />
          ) : (
            <Text size="xs" c="dimmed">
              {pull >= THRESHOLD * DAMPING ? '松手刷新' : '下拉刷新'}
            </Text>
          )}
        </Center>
      )}
      {children}
    </Box>
  );
}
