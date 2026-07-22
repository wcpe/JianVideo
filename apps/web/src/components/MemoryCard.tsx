import { useNavigate } from 'react-router-dom';
import { ActionIcon, Badge, Box, Card, Text } from '@mantine/core';
import { IconPlayerPlay } from '@tabler/icons-react';
import MediaThumbnail from '@/components/MediaThumbnail';
import { mediaDisplayName } from '@/utils/media';
import { mediaDayKey } from '@/utils/timeline';
import type { MediaFile } from '@/types';

interface MemoryCardProps {
  // 回忆媒体项
  file: MediaFile;
  // 无障碍标签前缀（如「最近查看」「那年今日」），拼接展示名标识卡片
  labelPrefix: string;
  // 可选角标（如「X 年前的今天」）
  badge?: string;
}

/**
 * 回忆区块单卡（FR-145）：供「最近查看」「那年今日」等回忆轮播复用，消除重复卡片结构。
 *
 * 卡片主体点击：跳到该媒体「那天」的时间轴并按日期筛选——导航到 /timeline?date=YYYY-MM-DD，
 * 由时间轴页读取 URL query 初始化（复用 FR-142 日期定位）。无有效日期时退化为直接播放。
 * 右上角提供独立播放入口：点击进入播放页，不触发跳转（stopPropagation）。
 */
export default function MemoryCard({ file, labelPrefix, badge }: MemoryCardProps) {
  const navigate = useNavigate();
  const name = mediaDisplayName(file);
  const day = mediaDayKey(file);

  // 卡片主体点击：有「那天」则跳时间轴并按日期筛选，否则退化为播放
  const handleOpenDay = () => {
    if (day) navigate(`/timeline?date=${day}`);
    else navigate(`/play/${file.id}`);
  };

  return (
    <Card
      withBorder
      padding="xs"
      radius="md"
      role="button"
      aria-label={`${labelPrefix} ${name}`}
      onClick={handleOpenDay}
      style={{ width: 200, flexShrink: 0, cursor: 'pointer', scrollSnapAlign: 'start' }}
    >
      <Card.Section style={{ position: 'relative' }}>
        <MediaThumbnail mediaID={file.id} fileName={file.file_name} />
        {/* 卡片播放入口（FR-145）：独立于主体跳转，点击直达播放页 */}
        <ActionIcon
          variant="filled"
          color="dark"
          size="sm"
          radius="xl"
          aria-label={`播放 ${name}`}
          title="播放"
          onClick={(e) => {
            e.stopPropagation();
            navigate(`/play/${file.id}`);
          }}
          style={{ position: 'absolute', top: 6, right: 6, zIndex: 2 }}
        >
          <IconPlayerPlay size={14} />
        </ActionIcon>
      </Card.Section>
      <Box mt="xs">
        <Text size="sm" fw={500} truncate>
          {name}
        </Text>
        {badge && (
          <Badge size="sm" variant="light" color="purple" mt={6}>
            {badge}
          </Badge>
        )}
      </Box>
    </Card>
  );
}
