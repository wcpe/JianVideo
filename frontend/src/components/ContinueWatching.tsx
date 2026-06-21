import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Box, Card, Group, Progress, Stack, Text, Title } from '@mantine/core'
import MediaThumbnail from '@/components/MediaThumbnail'
import * as libApi from '@/api/library'
import type { MediaFile } from '@/types'

/**
 * 首页「继续观看」区块（FR-44）。
 *
 * 拉取有进度且未看完的媒体，横向滚动展示缩略图卡片与续播进度条；
 * 点击进入播放页从上次位置续播。列表为空时整块不渲染。
 */
export default function ContinueWatching() {
  const navigate = useNavigate()
  const [items, setItems] = useState<MediaFile[]>([])

  const load = useCallback(() => {
    libApi.getContinueWatching()
      .then(setItems)
      .catch(() => setItems([]))
  }, [])

  useEffect(() => { load() }, [load])

  if (items.length === 0) return null

  return (
    <Stack gap="xs" data-testid="continue-watching">
      <Title order={4}>继续观看</Title>
      {/* 横向滚动容器：用原生溢出滚动，避免 Mantine ScrollArea 依赖 getComputedStyle */}
      <Box style={{ overflowX: 'auto', paddingBottom: 4 }}>
        <Group gap="sm" wrap="nowrap" align="stretch">
          {items.map((f) => {
            const pct = f.duration > 0 ? Math.min(100, ((f.last_position ?? 0) / f.duration) * 100) : 0
            return (
              <Card
                key={f.id}
                withBorder
                padding="xs"
                radius="md"
                role="button"
                aria-label={`继续观看 ${f.file_name}`}
                onClick={() => navigate(`/play/${f.id}`)}
                style={{ width: 200, flexShrink: 0, cursor: 'pointer' }}
              >
                <Card.Section>
                  <MediaThumbnail mediaID={f.id} fileName={f.file_name} />
                </Card.Section>
                <Box mt="xs">
                  <Text size="sm" fw={500} truncate>{f.file_name}</Text>
                  <Progress value={pct} size="xs" mt={6} color="purple" />
                </Box>
              </Card>
            )
          })}
        </Group>
      </Box>
    </Stack>
  )
}
