import type { ReactNode } from 'react'
import { Stack, Text, Button } from '@mantine/core'

/** 空态 CTA 配置：按钮文案、点击回调，可选加载态与左侧图标 */
export interface EmptyStateAction {
  label: string
  onClick: () => void
  loading?: boolean
  leftIcon?: ReactNode
}

export interface EmptyStateProps {
  /** 居中图形：不传则用内置原创空盒插画；可传 tabler 图标作居中图标 */
  icon?: ReactNode
  /** 主标题 */
  title: string
  /** 说明文案（可选） */
  description?: string
  /** 可选的一键 CTA 按钮 */
  action?: EmptyStateAction
}

/**
 * 原创空盒插画（FR-88）：简洁线性「打开的空盒子」意象，
 * 用 currentColor + 半透明描边，深浅主题皆可读，不依赖第三方插画库。
 */
function EmptyBoxIllustration() {
  return (
    <svg
      width={96}
      height={96}
      viewBox="0 0 96 96"
      fill="none"
      role="img"
      aria-label="空"
      style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }}
    >
      {/* 盒身：等距视角的敞口箱体 */}
      <path
        d="M16 40 L48 56 L80 40 L48 24 Z"
        stroke="currentColor"
        strokeWidth={2.5}
        strokeLinejoin="round"
        opacity={0.85}
      />
      <path
        d="M16 40 L16 64 L48 80 L48 56"
        stroke="currentColor"
        strokeWidth={2.5}
        strokeLinejoin="round"
        opacity={0.55}
      />
      <path
        d="M80 40 L80 64 L48 80"
        stroke="currentColor"
        strokeWidth={2.5}
        strokeLinejoin="round"
        opacity={0.55}
      />
      {/* 盒内漂浮的小圆点：暗示「这里本该有内容」 */}
      <circle cx={48} cy={20} r={3} fill="currentColor" opacity={0.45} />
      <circle cx={33} cy={26} r={2} fill="currentColor" opacity={0.3} />
      <circle cx={63} cy={26} r={2} fill="currentColor" opacity={0.3} />
    </svg>
  )
}

/**
 * 通用空态组件（FR-88）：居中插画/图标 + 标题 + 说明 + 可选 CTA。
 * 统一转码/巡检/重复项/回收站等页的「无数据」与统计页「空库」体验。
 */
export default function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <Stack align="center" gap="xs" py="xl" data-testid="empty-state">
      {icon ?? <EmptyBoxIllustration />}
      <Text fw={600} ta="center">{title}</Text>
      {description && <Text size="sm" c="dimmed" ta="center" maw={420}>{description}</Text>}
      {action && (
        <Button
          mt="sm"
          color="purple"
          variant="light"
          leftSection={action.leftIcon}
          loading={action.loading}
          onClick={action.onClick}
        >
          {action.label}
        </Button>
      )}
    </Stack>
  )
}
