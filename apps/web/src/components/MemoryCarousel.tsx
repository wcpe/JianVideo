import { useRef, useState, useCallback, useEffect, type ReactNode } from 'react';
import { ActionIcon, Box, Group, Stack, Title } from '@mantine/core';
import { IconChevronLeft, IconChevronRight } from '@tabler/icons-react';

interface MemoryCarouselProps {
  // 区块标题（如「最近查看」「那年今日」「继续观看」）
  title: string;
  // 外层 data-testid，沿用各回忆区块原有标识
  testId: string;
  // 卡片内容（横向排列的卡片列表）
  children: ReactNode;
}

// 单次翻页滚动比例：按可视宽度的 0.8 翻页，保留少量重叠便于衔接
const SCROLL_RATIO = 0.8;

/**
 * 回忆区块横向轮播容器（FR-145）。
 *
 * 用原生溢出滚动 + CSS scroll-snap 承载卡片横向滚动（避免 Mantine ScrollArea 依赖 getComputedStyle），
 * 叠加左右滚动按钮翻页。按钮可用性按轨道当前滚动位置评估：已到最左禁用左按钮、已到最右禁用右按钮，
 * 不可横向滚动（内容未超出）时两侧按钮均禁用。各回忆区块复用本容器，消除三处重复的滚动结构。
 */
export default function MemoryCarousel({ title, testId, children }: MemoryCarouselProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  // 左右按钮可用性：依据轨道 scrollLeft / scrollWidth / clientWidth 评估
  const [canLeft, setCanLeft] = useState(false);
  const [canRight, setCanRight] = useState(false);

  // 评估按钮可用性：留 1px 容差消除小数滚动位带来的抖动
  const evaluate = useCallback(() => {
    const el = trackRef.current;
    if (!el) return;
    const maxLeft = el.scrollWidth - el.clientWidth;
    setCanLeft(el.scrollLeft > 1);
    setCanRight(el.scrollLeft < maxLeft - 1);
  }, []);

  useEffect(() => {
    evaluate();
    const el = trackRef.current;
    if (!el) return;
    el.addEventListener('scroll', evaluate, { passive: true });
    // 容器尺寸变化（窗口缩放 / 内容增减）后重新评估
    const ro = new ResizeObserver(evaluate);
    ro.observe(el);
    return () => {
      el.removeEventListener('scroll', evaluate);
      ro.disconnect();
    };
  }, [evaluate, children]);

  // 翻页：按可视宽度比例平滑横向滚动；dir=1 向右、dir=-1 向左
  const scrollByPage = useCallback((dir: 1 | -1) => {
    const el = trackRef.current;
    if (!el) return;
    el.scrollBy({ left: dir * el.clientWidth * SCROLL_RATIO, behavior: 'smooth' });
  }, []);

  return (
    <Stack gap="xs" data-testid={testId}>
      <Group justify="space-between" align="center">
        <Title order={4}>{title}</Title>
        {/* 左右滚动按钮：到边界即禁用，不可滚动时均禁用 */}
        <Group gap={4}>
          <ActionIcon
            variant="default"
            size="sm"
            aria-label="向左滚动"
            disabled={!canLeft}
            onClick={() => scrollByPage(-1)}
          >
            <IconChevronLeft size={16} />
          </ActionIcon>
          <ActionIcon
            variant="default"
            size="sm"
            aria-label="向右滚动"
            disabled={!canRight}
            onClick={() => scrollByPage(1)}
          >
            <IconChevronRight size={16} />
          </ActionIcon>
        </Group>
      </Group>
      {/* 横向滚动轨道：原生溢出滚动 + scroll-snap 让卡片对齐停靠 */}
      <Box
        ref={trackRef}
        data-testid="carousel-track"
        style={{
          overflowX: 'auto',
          paddingBottom: 4,
          scrollSnapType: 'x mandatory',
        }}
      >
        <Group gap="sm" wrap="nowrap" align="stretch">
          {children}
        </Group>
      </Box>
    </Stack>
  );
}
