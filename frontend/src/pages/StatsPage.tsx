import { useState, useEffect } from 'react'
import { Stack, Title, Text, Group, Card, Progress, Loader, Center, SimpleGrid, Badge } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { Link } from 'react-router-dom'
import { getWatchStats } from '@/api/stats'
import { mediaDisplayName } from '@/utils/media'
import MediaThumbnail from '@/components/MediaThumbnail'
import type { WatchStats } from '@/types'

// 续播位置热力 10 档的区间标签（下标 0=0-10%…9=90-100%）
const HEATMAP_LABELS = ['0-10%', '10-20%', '20-30%', '30-40%', '40-50%', '50-60%', '60-70%', '70-80%', '80-90%', '90-100%']

// 按桶值占最大值的比例返回背景色（紫色系深浅）：0 值留浅底，值越大越深。
function heatColor(value: number, max: number): string {
  if (value <= 0 || max <= 0) return 'var(--mantine-color-default-border)'
  // 比例映射到 0.15~0.95 透明度，避免最小值过淡看不出
  const alpha = 0.15 + 0.8 * (value / max)
  return `rgba(151, 117, 250, ${alpha.toFixed(2)})`
}

/** 观看统计页（FR-75）：复用 FR-44 观看状态，自建无图表库展示各观看维度 */
export default function StatsPage() {
  const [stats, setStats] = useState<WatchStats | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    getWatchStats()
      .then((s) => { if (active) setStats(s) })
      .catch((err) => {
        notifications.show({ title: '加载失败', message: err instanceof Error ? err.message : '无法加载观看统计', color: 'red', autoClose: 3000 })
      })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  if (loading) {
    return <Center py="xl"><Loader color="purple" /></Center>
  }
  if (!stats) {
    return <Text c="dimmed">暂无观看统计数据。</Text>
  }

  const watchedPercent = stats.total > 0 ? Math.round((stats.watched / stats.total) * 100) : 0
  const heatmapMax = Math.max(0, ...stats.position_heatmap)
  const timelineMax = Math.max(0, ...stats.recent_timeline.map((b) => b.count))
  const libraryMax = Math.max(0, ...stats.by_library.map((l) => l.watched))
  const formatMax = Math.max(0, ...stats.by_format.map((f) => f.watched))
  const topMax = Math.max(0, ...stats.top_viewed.map((m) => m.view_count ?? 0))

  return (
    <Stack gap="lg">
      <Title order={2}>观看统计</Title>
      <Text size="sm" c="dimmed">基于观看状态（FR-44）汇总：看了多少、最近活跃度、续播停留位置、各库与各格式分布、最常看的视频。</Text>

      {/* 已看 / 未看占比 */}
      <Card withBorder padding="md" radius="md">
        <Group justify="space-between" mb="xs">
          <Text fw={600}>观看进度概览</Text>
          <Text size="sm" c="dimmed">已看 {stats.watched} / 共 {stats.total}（{watchedPercent}%）</Text>
        </Group>
        <Progress.Root size="xl">
          <Progress.Section value={watchedPercent} color="purple">
            <Progress.Label>已看 {stats.watched}</Progress.Label>
          </Progress.Section>
        </Progress.Root>
        <Group gap="lg" mt="xs">
          <Group gap={6}><Badge color="purple" variant="filled" size="sm">已看</Badge><Text size="sm">{stats.watched}</Text></Group>
          <Group gap={6}><Badge color="gray" variant="light" size="sm">未看</Badge><Text size="sm">{stats.unwatched}</Text></Group>
        </Group>
      </Card>

      {/* 续播位置热力（10 档） */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">续播位置热力</Text>
        <Text size="xs" c="dimmed" mb="sm">有续播进度的视频停留在播放时长哪个区间——越深表示该区间停留的视频越多。</Text>
        {heatmapMax === 0 ? (
          <Text size="sm" c="dimmed">暂无续播进度数据。</Text>
        ) : (
          <SimpleGrid cols={{ base: 5, sm: 10 }} spacing={6}>
            {stats.position_heatmap.map((value, i) => (
              <Stack key={i} gap={2} align="center">
                <div
                  data-testid={`heat-cell-${i}`}
                  aria-label={`${HEATMAP_LABELS[i]} ${value} 个`}
                  style={{ width: '100%', height: 40, borderRadius: 6, background: heatColor(value, heatmapMax), display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                >
                  <Text size="sm" fw={600}>{value}</Text>
                </div>
                <Text size="xs" c="dimmed">{HEATMAP_LABELS[i]}</Text>
              </Stack>
            ))}
          </SimpleGrid>
        )}
      </Card>

      {/* 最近观看时间线 */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">最近观看时间线</Text>
        {stats.recent_timeline.length === 0 ? (
          <Text size="sm" c="dimmed">暂无观看记录。</Text>
        ) : (
          <Group gap={6} align="flex-end" wrap="wrap">
            {stats.recent_timeline.map((b) => (
              <Stack key={b.date} gap={2} align="center">
                <div
                  aria-label={`${b.date} 观看 ${b.count} 个`}
                  style={{ width: 18, height: 70, borderRadius: 4, background: 'var(--mantine-color-default-border)', display: 'flex', alignItems: 'flex-end', overflow: 'hidden' }}
                >
                  <div style={{ width: '100%', height: `${timelineMax > 0 ? Math.round((b.count / timelineMax) * 100) : 0}%`, background: 'var(--mantine-color-purple-5)' }} />
                </div>
                <Text size="xs" c="dimmed" style={{ writingMode: 'vertical-rl' }}>{b.date.slice(5)}</Text>
              </Stack>
            ))}
          </Group>
        )}
      </Card>

      {/* 各库 / 各格式分布 */}
      <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="xs">各存储库已看分布</Text>
          {stats.by_library.length === 0 ? (
            <Text size="sm" c="dimmed">暂无已看媒体。</Text>
          ) : (
            <Stack gap="xs">
              {stats.by_library.map((l) => (
                <div key={l.library_id}>
                  <Group justify="space-between" mb={2}>
                    <Text size="sm" truncate>{l.label || `库 #${l.library_id}`}</Text>
                    <Text size="sm" c="dimmed">{l.watched}</Text>
                  </Group>
                  <Progress value={libraryMax > 0 ? (l.watched / libraryMax) * 100 : 0} color="purple" size="sm" />
                </div>
              ))}
            </Stack>
          )}
        </Card>

        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="xs">各格式已看分布</Text>
          {stats.by_format.length === 0 ? (
            <Text size="sm" c="dimmed">暂无已看媒体。</Text>
          ) : (
            <Stack gap="xs">
              {stats.by_format.map((f) => (
                <div key={f.format || 'unknown'}>
                  <Group justify="space-between" mb={2}>
                    <Text size="sm" truncate>{f.format || '未知格式'}</Text>
                    <Text size="sm" c="dimmed">{f.watched}</Text>
                  </Group>
                  <Progress value={formatMax > 0 ? (f.watched / formatMax) * 100 : 0} color="grape" size="sm" />
                </div>
              ))}
            </Stack>
          )}
        </Card>
      </SimpleGrid>

      {/* 观看次数 Top 榜 */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">观看次数 Top 榜</Text>
        {stats.top_viewed.length === 0 ? (
          <Text size="sm" c="dimmed">暂无看完记录。看完视频后这里会出现榜单。</Text>
        ) : (
          <Stack gap="xs">
            {stats.top_viewed.map((m, i) => (
              <Card key={m.id} withBorder padding="xs" radius="md">
                <Group justify="space-between" wrap="nowrap">
                  <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
                    <Text fw={700} c="dimmed" w={24} ta="center">#{i + 1}</Text>
                    <Link to={`/play/${m.id}`} style={{ textDecoration: 'none', minWidth: 0 }}>
                      <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
                        <div style={{ width: 64, flexShrink: 0 }}>
                          <MediaThumbnail mediaID={m.id} fileName={m.file_name} />
                        </div>
                        <Text size="sm" truncate title={mediaDisplayName(m)}>{mediaDisplayName(m)}</Text>
                      </Group>
                    </Link>
                  </Group>
                  <Badge color="purple" variant="light" style={{ flexShrink: 0 }}>{m.view_count ?? 0} 次</Badge>
                </Group>
                <Progress value={topMax > 0 ? ((m.view_count ?? 0) / topMax) * 100 : 0} color="purple" size="xs" mt={6} />
              </Card>
            ))}
          </Stack>
        )}
      </Card>
    </Stack>
  )
}
