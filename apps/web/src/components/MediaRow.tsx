import { Card, Group, Stack, Text, Checkbox, Box } from '@mantine/core';
import type { ReactNode } from 'react';
import MediaThumbnail from '@/components/MediaThumbnail';

interface MediaRowProps {
  // 缩略图所需媒体标识与文件名；为空（如巡检页源文件已丢失的问题项）时不渲染缩略图。
  mediaID?: number;
  thumbFileName?: string;
  // 行主标题（展示名 / 文件名）与可选副标题（详情 / 路径等）。
  title: string;
  // 主标题悬停全文提示（长文件名 / 路径），不传则用 title 文本本身。
  titleTooltip?: string;
  subtitle?: string;
  subtitleTooltip?: string;
  // 勾选模式：传 selectable 时渲染左侧复选框，受控于 selected/onToggle。
  selectable?: boolean;
  selected?: boolean;
  onToggle?: () => void;
  // 复选框无障碍标签（如「选择 xxx.mp4」），勾选模式下必传。
  checkboxLabel?: string;
  // 右侧操作区（还原按钮等）。
  actions?: ReactNode;
}

/**
 * 统一媒体行（FR-101）：缩略图 + 信息（主/副标题）+ 勾选/操作，
 * 供回收站 / 巡检 / 重复项三页复用，消解三页媒体行各自手写、设计不一致的问题。
 * 直接复用现有 MediaThumbnail（不动其 API/实现）；保留 .mantine-Card-root 类与无障碍标签兼容既有定位。
 */
export default function MediaRow({
  mediaID,
  thumbFileName,
  title,
  titleTooltip,
  subtitle,
  subtitleTooltip,
  selectable,
  selected,
  onToggle,
  checkboxLabel,
  actions,
}: MediaRowProps) {
  return (
    <Card withBorder padding="xs" radius="md" className="mantine-Card-root">
      <Group justify="space-between" wrap="nowrap" gap="sm">
        <Group gap="sm" wrap="nowrap" style={{ minWidth: 0, flex: 1 }}>
          {selectable && (
            <Checkbox
              checked={!!selected}
              onChange={onToggle}
              aria-label={checkboxLabel}
              style={{ flexShrink: 0 }}
            />
          )}
          {mediaID !== undefined && (
            // 缩略图定宽缩略块，复用 MediaThumbnail；占位不挤压文字区。
            <Box style={{ width: 72, flexShrink: 0 }}>
              <MediaThumbnail mediaID={mediaID} fileName={thumbFileName ?? title} />
            </Box>
          )}
          <Stack gap={2} style={{ minWidth: 0, flex: 1 }}>
            <Text size="sm" truncate title={titleTooltip ?? title}>
              {title}
            </Text>
            {subtitle && (
              <Text size="xs" c="dimmed" truncate title={subtitleTooltip ?? subtitle}>
                {subtitle}
              </Text>
            )}
          </Stack>
        </Group>
        {actions && <Box style={{ flexShrink: 0 }}>{actions}</Box>}
      </Group>
    </Card>
  );
}
