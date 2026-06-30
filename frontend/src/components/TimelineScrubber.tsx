import { useRef, useState, useCallback, useEffect } from 'react';
import { Box, Paper, SimpleGrid, Stack, Text, UnstyledButton } from '@mantine/core';
import MediaThumbnail from '@/components/MediaThumbnail';
import { positionToGroupIndex } from '@/utils/timeline';
import type { DateGroup } from '@/utils/timeline';

interface TimelineScrubberProps {
  // 当前分组列表（已按 granularity 分组、倒序），顶部=最新、底部=最旧
  groups: DateGroup[];
  // 松手 / 方向键确定目标分组时回调，参数为目标分组下标
  onSeek: (index: number) => void;
  // 窄屏防误触形态（FR-143）：true 时隐藏 12px 可拖动细轨（屏幕边缘易误触），
  // 改渲染可点击年份标签快速导航（点年份跳到该年最新分组）。
  compact?: boolean;
}

// 浮层九宫格展示的缩略图上限（前 N 张）
const PREVIEW_THUMBS = 6;

/**
 * 单个刻度标记：纵向比例位置（0=顶/最新，1=底/最旧）+ 显示文本（年份）+ 该年最新分组下标。
 * index 供窄屏年份快速导航点击跳转复用（FR-143）。
 */
interface TimelineTick {
  fraction: number;
  label: string;
  index: number;
}

/**
 * 由分组列表计算年份刻度（FR-138 + FR-143）。
 * - 分组已倒序（顶部=最新）：每个年份在其首次出现处放一个年份刻度（即该年最新分组）。
 * - 刻度纵向位置 = 该分组下标 / 分组总数，与轨道 hover/拖动同一映射口径。
 * - index 记录该年首次出现（最新）分组的下标，供窄屏点击年份跳转（FR-143）。
 * 纯函数，无副作用，便于穷举测试。
 */
function computeYearTicks(groups: DateGroup[]): TimelineTick[] {
  const ticks: TimelineTick[] = [];
  const seen = new Set<string>();
  const count = groups.length;
  if (count === 0) return ticks;
  groups.forEach((g, i) => {
    // 分组键形如 YYYY / YYYY-MM / YYYY-MM-DD，取前 4 位作年份；非法日期（如「未知日期」）跳过
    const year = g.date.slice(0, 4);
    if (!/^\d{4}$/.test(year) || seen.has(year)) return;
    seen.add(year);
    ticks.push({ fraction: i / count, label: year, index: i });
  });
  return ticks;
}

/**
 * 时间轴时间标尺 scrubber（FR-68 + FR-120 + FR-138）：右侧竖向轨道，作为唯一完整时间轴导航条。
 *
 * - 常驻年份刻度（FR-138）：轨道旁按 fraction 纵向布点，不交互时也能看出覆盖的时间段。
 * - hover（指针未按下）即按指针 Y 算目标分组、在指针处弹出预览浮层（分组日期 + 该组数量 +
 *   前若干张缩略图九宫格）；移出轨道隐藏（FR-120 苹果风）。
 * - 拖动（按下）持续更新预览并松手跳转到对应分组（FR-68 保留）。
 * - 键盘可达：上下方向键移动分组并跳转（FR-68 保留）。
 *
 * hover 与 drag 共用「指针 Y → 分组下标」纯函数 positionToGroupIndex；指针移动用 rAF 前沿节流，
 * 避免高频重渲染抖动。预览浮层 pointer-events:none + role="img" + aria-label（日期 + 数量）。
 */
export default function TimelineScrubber({ groups, onSeek, compact = false }: TimelineScrubberProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  // 预览下标（null 表示不显示浮层，hover 与 drag 共用）
  const [previewIndex, setPreviewIndex] = useState<number | null>(null);
  // 是否拖动中（指针已按下捕获）：用于区分 hover 与 drag，决定移出轨道时是否清预览
  const [dragging, setDragging] = useState(false);
  // 浮层纵向位置（指针在轨道内的像素 y），用于浮层贴着指针
  const [pointerY, setPointerY] = useState(0);

  // rAF 前沿节流：最新指针 Y 暂存于 ref，已排帧标记避免一帧内重复处理
  const latestY = useRef(0);
  const frame = useRef(0);

  const count = groups.length;

  // 由指针 clientY 计算目标分组下标并更新预览（纯计算 + setState，无副作用泄漏）
  const applyFromPointer = useCallback(
    (clientY: number): number => {
      const el = trackRef.current;
      if (!el || count === 0) return 0;
      const rect = el.getBoundingClientRect();
      const offset = clientY - rect.top;
      const fraction = rect.height > 0 ? offset / rect.height : 0;
      const index = positionToGroupIndex(fraction, count);
      setPreviewIndex(index);
      setPointerY(Math.min(rect.height, Math.max(0, offset)));
      return index;
    },
    [count],
  );

  // rAF 前沿节流封装：首次（无待处理帧）立即处理保证响应即时，
  // 同帧内后续移动仅更新暂存 Y，由下一帧统一处理最新值，避免高频重渲染。
  const scheduleFromPointer = useCallback(
    (clientY: number) => {
      latestY.current = clientY;
      if (frame.current) return;
      applyFromPointer(clientY);
      frame.current = requestAnimationFrame(() => {
        frame.current = 0;
        // 帧间若指针又移动过，补处理最新值
        if (latestY.current !== clientY) applyFromPointer(latestY.current);
      });
    },
    [applyFromPointer],
  );

  // 卸载时清理待处理帧，避免泄漏
  useEffect(
    () => () => {
      if (frame.current) cancelAnimationFrame(frame.current);
    },
    [],
  );

  const handlePointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      // 捕获指针，拖出轨道范围仍持续跟手
      e.currentTarget.setPointerCapture?.(e.pointerId);
      setDragging(true);
      applyFromPointer(e.clientY);
    },
    [applyFromPointer],
  );

  const handlePointerMove = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      // hover 与 drag 均更新预览（拖动中跟手、未按下时随 hover 弹出）
      scheduleFromPointer(e.clientY);
    },
    [scheduleFromPointer],
  );

  const handlePointerUp = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (!dragging) return;
      const index = applyFromPointer(e.clientY);
      e.currentTarget.releasePointerCapture?.(e.pointerId);
      setDragging(false);
      // 松手后回到 hover 态：指针仍在轨道上，保留预览不清空
      onSeek(index);
    },
    [dragging, applyFromPointer, onSeek],
  );

  // 指针移出轨道：非拖动态隐藏预览（拖动态由 pointerUp 收尾，跟手不中断）
  const handlePointerLeave = useCallback(() => {
    if (dragging) return;
    setPreviewIndex(null);
  }, [dragging]);

  // 键盘可达：上下方向键移动一个分组并跳转（顶部=最新=下标 0）
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      if (count === 0) return;
      const base = previewIndex ?? 0;
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        const next = Math.min(count - 1, base + 1);
        setPreviewIndex(next);
        onSeek(next);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        const next = Math.max(0, base - 1);
        setPreviewIndex(next);
        onSeek(next);
      }
    },
    [previewIndex, count, onSeek],
  );

  if (count === 0) return null;

  // 窄屏防误触形态（FR-143）：隐藏 12px 可拖动细轨（屏幕边缘易误触），
  // 改渲染竖排可点击年份标签，点年份跳到该年最新分组（复用 onSeek + tick.index）。
  if (compact) {
    const ticks = computeYearTicks(groups);
    if (ticks.length === 0) return null;
    return (
      <Box
        data-testid="timeline-year-nav"
        style={{ position: 'absolute', top: 0, right: -16, height: '100%' }}
      >
        <Stack gap={4} style={{ position: 'sticky', top: 8 }}>
          {ticks.map((tick) => (
            <UnstyledButton
              key={tick.label}
              aria-label={`跳到 ${tick.label} 年`}
              onClick={() => onSeek(tick.index)}
              style={{
                padding: '4px 6px',
                borderRadius: 6,
                background: 'var(--mantine-color-default)',
                border: '1px solid var(--mantine-color-default-border)',
                lineHeight: 1,
              }}
            >
              <Text size="xs" c="dimmed">
                {tick.label}
              </Text>
            </UnstyledButton>
          ))}
        </Stack>
      </Box>
    );
  }

  const activeGroup = previewIndex !== null ? groups[previewIndex] : null;
  const previewThumbs = activeGroup ? activeGroup.files.slice(0, PREVIEW_THUMBS) : [];
  // 常驻年份刻度（FR-138）：不交互时也能看出当前轨道覆盖的时间段
  const yearTicks = computeYearTicks(groups);

  return (
    <Box
      ref={trackRef}
      role="slider"
      aria-label="时间轴拖动定位"
      aria-valuemin={0}
      aria-valuemax={count - 1}
      aria-valuenow={previewIndex ?? 0}
      aria-valuetext={activeGroup?.date}
      tabIndex={0}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerLeave={handlePointerLeave}
      onKeyDown={handleKeyDown}
      style={{
        position: 'absolute',
        top: 0,
        right: -16,
        width: 12,
        height: '100%',
        cursor: 'ns-resize',
        borderRadius: 6,
        background: 'var(--mantine-color-default-border)',
        touchAction: 'none',
      }}
    >
      {/* 常驻年份刻度层（FR-138）：贴轨道左侧按 fraction 纵向布点，pointer-events:none 不挡轨道交互。
          作为唯一完整时间轴的静态参照，hover/拖动浮层叠加其上。 */}
      <Box
        data-testid="timeline-ticks"
        aria-hidden
        style={{
          position: 'absolute',
          top: 0,
          right: 16,
          height: '100%',
          pointerEvents: 'none',
        }}
      >
        {yearTicks.map((tick) => (
          <Text
            key={tick.label}
            size="xs"
            c="dimmed"
            style={{
              position: 'absolute',
              top: `${tick.fraction * 100}%`,
              right: 0,
              transform: 'translateY(-50%)',
              whiteSpace: 'nowrap',
              lineHeight: 1,
            }}
          >
            {tick.label}
          </Text>
        ))}
      </Box>

      {/* 预览浮层（FR-120）：贴着指针纵向位置，展示目标分组日期 + 数量 + 前若干张缩略图九宫格。
          pointer-events:none 不挡轨道交互；role="img" + aria-label 暴露日期与数量给无障碍。 */}
      {activeGroup && (
        <Paper
          shadow="md"
          p="xs"
          radius="md"
          withBorder
          role="img"
          aria-label={`${activeGroup.date} 共 ${activeGroup.files.length} 项`}
          style={{
            position: 'absolute',
            right: 20,
            top: pointerY,
            transform: 'translateY(-50%)',
            width: 188,
            pointerEvents: 'none',
            background: 'var(--mantine-color-default)',
            zIndex: 10,
          }}
        >
          <Text fw={600} size="sm" ta="center">
            {activeGroup.date}
          </Text>
          <Text c="dimmed" size="xs" ta="center" mb={6}>
            {activeGroup.files.length} 项
          </Text>
          {/* 缩略图九宫格：3 列密铺，最多前 6 张 */}
          <SimpleGrid cols={3} spacing={4} verticalSpacing={4}>
            {previewThumbs.map((f) => (
              <Box key={f.id} style={{ aspectRatio: '1', overflow: 'hidden', borderRadius: 4 }}>
                <MediaThumbnail mediaID={f.id} fileName={f.file_name} objectFit="cover" />
              </Box>
            ))}
          </SimpleGrid>
        </Paper>
      )}
    </Box>
  );
}
