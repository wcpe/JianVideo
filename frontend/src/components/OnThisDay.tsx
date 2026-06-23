import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Badge, Box, Card, Group, Stack, Text, Title } from '@mantine/core'
import MediaThumbnail from '@/components/MediaThumbnail'
import * as libApi from '@/api/library'
import { mediaDisplayName } from '@/utils/media'
import type { MediaFile } from '@/types'

/**
 * 计算「X 年前的今天」文案（FR-72）：今年减媒体年份。
 * media_time 为空或无法解析时返回空串（不展示标签）。
 */
function yearsAgoLabel(mediaTime?: string | null): string {
  if (!mediaTime) return ''
  const t = new Date(mediaTime)
  if (Number.isNaN(t.getTime())) return ''
  const diff = new Date().getFullYear() - t.getFullYear()
  return diff > 0 ? `${diff} 年前的今天` : ''
}

/**
 * 首页「那年今日」回忆区块（FR-72）。
 *
 * 拉取往年同一天（媒体时间命中今天月-日、年份非今年）拍摄的媒体，
 * 横向滚动展示缩略图卡片，每项标注「X 年前的今天」；点击进入播放页。
 * 列表为空时整块不渲染。
 */
export default function OnThisDay() {
  const navigate = useNavigate()
  const [items, setItems] = useState<MediaFile[]>([])

  const load = useCallback(() => {
    libApi.getOnThisDay()
      .then(setItems)
      .catch(() => setItems([]))
  }, [])

  useEffect(() => { load() }, [load])

  if (items.length === 0) return null

  return (
    <Stack gap="xs" data-testid="on-this-day">
      <Title order={4}>那年今日</Title>
      {/* 横向滚动容器：用原生溢出滚动，避免 Mantine ScrollArea 依赖 getComputedStyle */}
      <Box style={{ overflowX: 'auto', paddingBottom: 4 }}>
        <Group gap="sm" wrap="nowrap" align="stretch">
          {items.map((f) => {
            const name = mediaDisplayName(f)
            const label = yearsAgoLabel(f.media_time)
            return (
              <Card
                key={f.id}
                withBorder
                padding="xs"
                radius="md"
                role="button"
                aria-label={`那年今日 ${name}`}
                onClick={() => navigate(`/play/${f.id}`)}
                style={{ width: 200, flexShrink: 0, cursor: 'pointer' }}
              >
                <Card.Section>
                  <MediaThumbnail mediaID={f.id} fileName={f.file_name} />
                </Card.Section>
                <Box mt="xs">
                  <Text size="sm" fw={500} truncate>{name}</Text>
                  {label && <Badge size="sm" variant="light" color="purple" mt={6}>{label}</Badge>}
                </Box>
              </Card>
            )
          })}
        </Group>
      </Box>
    </Stack>
  )
}
