import type { ReactNode } from 'react';
import { motion } from 'framer-motion';
import { useReducedMotion } from '@mantine/hooks';

interface MotionFadeProps {
  children: ReactNode;
  /** 进入动画时长（秒），默认 0.2s，与 FR-96 既有 CSS 渐入时长保持一致 */
  duration?: number;
  /** 透传到底层 div 的测试钩子 */
  'data-testid'?: string;
  className?: string;
}

/**
 * framer-motion 动效最小封装（FR-135，见 ADR-0048）：
 * 统一承载「淡入 + 轻微上移」进入动画，复杂编排留后续期。
 * 强制遵守 prefers-reduced-motion：开启「减少动态」时完全不下发任何动画，
 * 直接渲染静态内容（data-motion=static），满足无障碍守护。
 */
export default function MotionFade({
  children,
  duration = 0.2,
  className,
  'data-testid': testId,
}: MotionFadeProps) {
  // useReducedMotion 读取系统「减少动态」偏好；为真时禁用所有动效
  const reduced = useReducedMotion();

  if (reduced) {
    return (
      <div data-motion="static" data-testid={testId} className={className}>
        {children}
      </div>
    );
  }

  return (
    <motion.div
      data-motion="animate"
      data-testid={testId}
      className={className}
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration, ease: 'easeOut' }}
    >
      {children}
    </motion.div>
  );
}
