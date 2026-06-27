import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Box, Card, Group, Stack, Text, Title } from '@mantine/core'
import MediaThumbnail from '@/components/MediaThumbnail'
import * as libApi from '@/api/library'
import { mediaDisplayName } from '@/utils/media'
import type { MediaFile } from '@/types'

/**
 * 时间轴页「最近查看」回忆区块（FR-120）。
 *
 * 拉取最近打开过的媒体（图片 + 视频，按 last_viewed_at 倒序），横向滚动展示缩略图卡片
 * 与展示名；点击进入查看/播放。列表为空（或拉取失败）时整块不渲染。
 *
 * 与「继续观看」（FR-44，有进度未看完）维度不同：本区块按最近打开排序、不论进度/类型，
 * 二者可重叠展示、互不替代。
 */
export default function RecentlyViewed() {
  const navigate = useNavigate()
  const [items, setItems] = useState<MediaFile[]>([])

  const load = useCallback(() => {
    libApi.getRecentlyViewed()
      .then(setItems)
      .catch(() => setItems([]))
  }, [])

  useEffect(() => { load() }, [load])

  if (items.length === 0) return null

  return (
    <Stack gap="xs" data-testid="recently-viewed">
      <Title order={4}>最近查看</Title>
      {/* 横向滚动容器：用原生溢出滚动，避免 Mantine ScrollArea 依赖 getComputedStyle */}
      <Box style={{ overflowX: 'auto', paddingBottom: 4 }}>
        <Group gap="sm" wrap="nowrap" align="stretch">
          {items.map((f) => {
            const name = mediaDisplayName(f)
            return (
              <Card
                key={f.id}
                withBorder
                padding="xs"
                radius="md"
                role="button"
                aria-label={`最近查看 ${name}`}
                onClick={() => navigate(`/play/${f.id}`)}
                style={{ width: 200, flexShrink: 0, cursor: 'pointer' }}
              >
                <Card.Section>
                  <MediaThumbnail mediaID={f.id} fileName={f.file_name} />
                </Card.Section>
                <Box mt="xs">
                  <Text size="sm" fw={500} truncate>{name}</Text>
                </Box>
              </Card>
            )
          })}
        </Group>
      </Box>
    </Stack>
  )
}
