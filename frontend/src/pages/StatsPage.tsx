import { useState, useEffect } from 'react'
import { Stack, Title, Text, Group, Card, Progress, Loader, Center, SimpleGrid, Badge, Tabs } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { IconChartBar, IconEye, IconPhoto } from '@tabler/icons-react'
import { getWatchStats } from '@/api/stats'
import { getLibrarySummary } from '@/api/summary'
import { getMediaTrends } from '@/api/trends'
import { getContinueWatching } from '@/api/library'
import { mediaDisplayName } from '@/utils/media'
import { formatBytes } from '@/utils/format'
import MediaThumbnail from '@/components/MediaThumbnail'
import EmptyState from '@/components/EmptyState'
import MetricCard from '@/components/MetricCard'
import TrendChart from '@/components/TrendChart'
import type { WatchStats, LibrarySummary, MediaTrends, MediaTrendPoint } from '@/types'

// 续播位置热力 10 档的区间标签（下标 0=0-10%…9=90-100%）
const HEATMAP_LABELS = ['0-10%', '10-20%', '20-30%', '30-40%', '40-50%', '50-60%', '60-70%', '70-80%', '80-90%', '90-100%']

// 一级 tab 取值（FR-118）：观看 / 媒体。非法一律落回 watch（仿 ConsolePage）。
const TAB_WATCH = 'watch'
const TAB_MEDIA = 'media'
const ALL_TABS = [TAB_WATCH, TAB_MEDIA]

// 按桶值占最大值的比例返回背景色（品牌紫阶深浅，FR-101）：0 值留浅底，值越大越深。
// 用品牌紫 token（--mantine-color-purple-6）经 color-mix 与透明叠加，避免写死 rgba 魔法值、随主题色板统一。
function heatColor(value: number, max: number): string {
  if (value <= 0 || max <= 0) return 'var(--mantine-color-default-border)'
  // 比例映射到 25%~100% 不透明度（最小值不过淡看不出）
  const pct = Math.round((0.25 + 0.75 * (value / max)) * 100)
  return `color-mix(in srgb, var(--mantine-color-purple-6) ${pct}%, transparent)`
}

// 把 URL query 归一为一级 tab key；非法（缺省 / 未知）落回 watch
function resolveTab(params: URLSearchParams): string {
  const tab = params.get('tab')
  return tab && ALL_TABS.includes(tab) ? tab : TAB_WATCH
}

/** 媒体增长累计点（FR-118）：把按天新增升序累加成累计值 */
interface CumulativePoint {
  date: string
  count: number
  size: number
  duration: number
}

/** 把按天新增（升序）累加为累计序列：count/size/duration 各自前缀和（增长曲线从 0 起累加） */
function accumulate(points: MediaTrendPoint[]): CumulativePoint[] {
  let c = 0
  let s = 0
  let d = 0
  return points.map((p) => {
    c += p.count
    s += p.size
    d += p.duration
    return { date: p.date, count: c, size: s, duration: d }
  })
}

/**
 * 统计页（FR-75 + FR-118）：顶部分「观看 / 媒体」两 tab（`?tab=` 记忆，仿 ConsolePage）。
 * - 观看 tab：当前值卡（已看/未看/追看中）+ 观看活跃趋势折线 + 既有续播热力/各库各格式/Top 榜（FR-75 原样保留）。
 * - 媒体 tab：当前值卡（总数/视频/图片/时长/占用）+ 媒体增长 / 累计容量时长折线 + 媒体构成（复用 FR-117 summary）。
 * 各数据源独立 try/catch 降级；空库整页空态、空趋势显占位不报错（沿用 FR-88）。
 */
export default function StatsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab = resolveTab(searchParams)

  const [stats, setStats] = useState<WatchStats | null>(null)
  const [summary, setSummary] = useState<LibrarySummary | null>(null)
  const [trends, setTrends] = useState<MediaTrends | null>(null)
  const [continueCount, setContinueCount] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    // 各数据源独立 try/catch 降级：观看统计为主（决定整页空态），其余失败仅置空、不阻断
    getWatchStats()
      .then((s) => { if (active) setStats(s) })
      .catch((err) => {
        notifications.show({ title: '加载失败', message: err instanceof Error ? err.message : '无法加载观看统计', color: 'red', autoClose: 3000 })
      })
      .finally(() => { if (active) setLoading(false) })
    getLibrarySummary().then((s) => { if (active) setSummary(s) }).catch(() => { if (active) setSummary(null) })
    getMediaTrends().then((t) => { if (active) setTrends(t) }).catch(() => { if (active) setTrends(null) })
    getContinueWatching().then((items) => { if (active) setContinueCount(items.length) }).catch(() => { if (active) setContinueCount(null) })
    return () => { active = false }
  }, [])

  // 切 tab 时写回 `?tab=<key>`；replace 避免历史堆叠切换记录
  const handleTabChange = (value: string | null) => {
    setSearchParams({ tab: value ?? TAB_WATCH }, { replace: true })
  }

  if (loading) {
    return <Center py="xl"><Loader color="purple" /></Center>
  }
  if (!stats) {
    return <Text c="dimmed">暂无观看统计数据。</Text>
  }

  // 空库：媒体库还没有任何媒体——整页引导去添加媒体库，不渲染任何空图表卡
  if (stats.total === 0) {
    return (
      <Stack gap="lg">
        <Title order={2}>统计</Title>
        <EmptyState
          icon={<IconChartBar size={72} stroke={1.2} style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }} />}
          title="还没有可统计的媒体"
          description="媒体库为空时这里没有数据。先去添加媒体库并扫描，观看后即可看到看了多少、最近活跃度、续播停留位置等统计。"
          action={{ label: '去添加媒体库', onClick: () => navigate('/library-manager') }}
        />
      </Stack>
    )
  }

  return (
    <Stack gap="lg">
      <Title order={2}>统计</Title>
      <Tabs value={activeTab} onChange={handleTabChange} keepMounted={false}>
        <Tabs.List mb="md">
          <Tabs.Tab value={TAB_WATCH} leftSection={<IconEye size={16} />}>观看</Tabs.Tab>
          <Tabs.Tab value={TAB_MEDIA} leftSection={<IconPhoto size={16} />}>媒体</Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value={TAB_WATCH}>
          <WatchTab stats={stats} continueCount={continueCount} />
        </Tabs.Panel>
        <Tabs.Panel value={TAB_MEDIA}>
          <MediaTab summary={summary} trends={trends} />
        </Tabs.Panel>
      </Tabs>
    </Stack>
  )
}

/** 观看 tab：当前值卡 + 观看活跃趋势 + 既有 FR-75 各卡（原样保留、逐卡空守卫不动） */
function WatchTab({ stats, continueCount }: { stats: WatchStats; continueCount: number | null }) {
  const watchedPercent = stats.total > 0 ? Math.round((stats.watched / stats.total) * 100) : 0
  const heatmapMax = Math.max(0, ...stats.position_heatmap)
  const timelineMax = Math.max(0, ...stats.recent_timeline.map((b) => b.count))
  const libraryMax = Math.max(0, ...stats.by_library.map((l) => l.watched))
  const formatMax = Math.max(0, ...stats.by_format.map((f) => f.watched))
  const topMax = Math.max(0, ...stats.top_viewed.map((m) => m.view_count ?? 0))
  // 稀疏态：有媒体但毫无观看活动（未看、无续播、无时间线）——隐藏满 0 的续播热力网格与孤立时间线细条
  const noActivity = stats.watched === 0 && heatmapMax === 0 && stats.recent_timeline.length === 0
  // 观看活跃趋势：后端按倒序返回，折线需升序——反转后按天观看数画线
  const activityData = [...stats.recent_timeline].reverse()
  // sparkline 序列（升序观看数）；过短时 MetricCard 自动退化为不显
  const activitySpark = activityData.map((b) => b.count)

  return (
    <Stack gap="lg">
      <Text size="sm" c="dimmed">基于观看状态（FR-44）汇总：看了多少、最近活跃度、续播停留位置、各库与各格式分布、最常看的视频。</Text>

      {/* 当前值卡：已看 / 未看 / 追看中 */}
      <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="md">
        <MetricCard label="已看" value={stats.watched.toLocaleString()} sparkline={activitySpark} />
        <MetricCard label="未看" value={stats.unwatched.toLocaleString()} />
        <MetricCard label="追看中" value={(continueCount ?? 0).toLocaleString()} />
      </SimpleGrid>

      {/* 已看 / 未看占比 */}
      <Card withBorder padding="md" radius="md">
        <Group justify="space-between" mb="xs">
          <Text fw={600}>观看进度概览</Text>
          <Text size="sm" c="dimmed">已看 {stats.watched} / 共 {stats.total}（{watchedPercent}%）</Text>
        </Group>
        <Progress.Root size="xl">
          <Progress.Section value={watchedPercent} color="purple" aria-label={`观看进度 ${watchedPercent}%`}>
            <Progress.Label>已看 {stats.watched}</Progress.Label>
          </Progress.Section>
        </Progress.Root>
        <Group gap="lg" mt="xs">
          <Group gap={6}><Badge color="purple" variant="filled" size="sm">已看</Badge><Text size="sm">{stats.watched}</Text></Group>
          {/* 灰色 light 徽标文字默认取 gray.6(#868e96)，落浅灰底仅约 2.85:1 不达 AA；强制 label 为更深 gray.7（FR-97 对比度修复） */}
          <Group gap={6}><Badge color="gray" variant="light" size="sm" styles={{ label: { color: 'var(--mantine-color-gray-7)' } }}>未看</Badge><Text size="sm">{stats.unwatched}</Text></Group>
        </Group>
      </Card>

      {/* 观看活跃趋势（FR-118）：复用 recent_timeline 按天观看数，升序折线，可 hover 看每点精确值 */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">观看活跃趋势</Text>
        <Text size="xs" c="dimmed" mb="sm">按天的观看媒体数，反映最近观看活跃度。</Text>
        <TrendChart
          data={activityData as unknown as Array<Record<string, unknown>>}
          xKey="date"
          series={[{ dataKey: 'count', label: '观看数' }]}
        />
      </Card>

      {/* 续播位置热力（10 档）：稀疏态（无任何观看活动）下整卡满 0 无意义，隐藏 */}
      {!noActivity && (
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
                  role="img"
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
      )}

      {/* 最近观看时间线：稀疏态下只有孤立细条或空文案，隐藏 */}
      {!noActivity && (
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">最近观看时间线</Text>
        {stats.recent_timeline.length === 0 ? (
          <Text size="sm" c="dimmed">暂无观看记录。</Text>
        ) : (
          <Group gap={6} align="flex-end" wrap="wrap">
            {stats.recent_timeline.map((b) => (
              <Stack key={b.date} gap={2} align="center">
                <div
                  role="img"
                  aria-label={`${b.date} 观看 ${b.count} 个`}
                  style={{ width: 20, height: 72, borderRadius: 4, background: 'var(--mantine-color-default-border)', display: 'flex', alignItems: 'flex-end', overflow: 'hidden' }}
                >
                  {/* 品牌紫条（FR-101）：以品牌锚点 purple-6 统一时间线条色 */}
                  <div style={{ width: '100%', height: `${timelineMax > 0 ? Math.round((b.count / timelineMax) * 100) : 0}%`, background: 'var(--mantine-color-purple-6)', borderRadius: '4px 4px 0 0', transition: 'height 200ms ease' }} />
                </div>
                <Text size="xs" c="dimmed" style={{ writingMode: 'vertical-rl' }}>{b.date.slice(5)}</Text>
              </Stack>
            ))}
          </Group>
        )}
      </Card>
      )}

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
                  <Progress value={libraryMax > 0 ? (l.watched / libraryMax) * 100 : 0} color="purple" size="sm" aria-label={`${l.label || `库 #${l.library_id}`} 已看 ${l.watched}`} />
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
                  <Progress value={formatMax > 0 ? (f.watched / formatMax) * 100 : 0} color="purple" size="sm" aria-label={`${f.format || '未知格式'} 已看 ${f.watched}`} />
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
                <Progress value={topMax > 0 ? ((m.view_count ?? 0) / topMax) * 100 : 0} color="purple" size="xs" mt={6} aria-label={`${mediaDisplayName(m)} 观看 ${m.view_count ?? 0} 次`} />
              </Card>
            ))}
          </Stack>
        )}
      </Card>
    </Stack>
  )
}

/** 媒体 tab：当前值卡 + 媒体增长 / 累计容量时长折线 + 媒体构成（复用 FR-117 summary 与 FR-118 trends） */
function MediaTab({ summary, trends }: { summary: LibrarySummary | null; trends: MediaTrends | null }) {
  if (!summary) {
    return <Text size="sm" c="dimmed">暂无媒体概览数据。</Text>
  }

  // 累计增长：把按天新增（升序）累加为累计 count/size/duration
  const cumulative = accumulate(trends?.media_added ?? [])
  // sparkline 序列（累计末端窗口）；空时各卡自动退化为不显
  const countSpark = cumulative.map((p) => p.count)
  const sizeSpark = cumulative.map((p) => p.size)
  const durationSpark = cumulative.map((p) => p.duration)
  // 总时长→小时（保留 1 位）
  const totalHours = summary.total_duration > 0 ? (summary.total_duration / 3600).toFixed(1) : '0'
  // 媒体构成：视频 / 图片占比
  const composeTotal = summary.video_count + summary.image_count
  const videoPct = composeTotal > 0 ? Math.round((summary.video_count / composeTotal) * 100) : 0
  const imagePct = composeTotal > 0 ? 100 - videoPct : 0
  // 各库 media_count 横条最大值
  const libMax = Math.max(0, ...summary.by_library.map((l) => l.media_count))

  return (
    <Stack gap="lg">
      <Text size="sm" c="dimmed">媒体库整体规模与随时间的增长：媒体总量、构成、累计容量与时长。</Text>

      {/* 当前值卡：总数 / 视频 / 图片 / 总时长 / 占用（复用 FR-117 summary） */}
      <SimpleGrid cols={{ base: 2, sm: 3, md: 5 }} spacing="md">
        <MetricCard label="媒体总数" value={summary.total.toLocaleString()} sparkline={countSpark} />
        <MetricCard label="视频" value={summary.video_count.toLocaleString()} />
        <MetricCard label="图片" value={summary.image_count.toLocaleString()} />
        <MetricCard label="总时长" value={`${totalHours} 小时`} sparkline={durationSpark} />
        <MetricCard label="占用" value={formatBytes(summary.total_size)} sparkline={sizeSpark} />
      </SimpleGrid>

      {/* 媒体增长曲线（FR-118）：累计 count 升序折线 */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">媒体增长曲线</Text>
        <Text size="xs" c="dimmed" mb="sm">按天累计的媒体总数，反映媒体库随时间增长。</Text>
        <TrendChart
          data={cumulative as unknown as Array<Record<string, unknown>>}
          xKey="date"
          series={[{ dataKey: 'count', label: '累计媒体数' }]}
        />
      </Card>

      {/* 累计容量 / 时长增长（FR-118）：累计 size / duration 升序折线 */}
      <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="xs">累计容量增长</Text>
          <TrendChart
            data={cumulative as unknown as Array<Record<string, unknown>>}
            xKey="date"
            series={[{ dataKey: 'size', label: '累计容量' }]}
            valueFormatter={formatBytes}
          />
        </Card>
        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="xs">累计时长增长</Text>
          <TrendChart
            data={cumulative as unknown as Array<Record<string, unknown>>}
            xKey="date"
            series={[{ dataKey: 'duration', label: '累计时长' }]}
            valueFormatter={(v) => `${(v / 3600).toFixed(1)} 小时`}
          />
        </Card>
      </SimpleGrid>

      {/* 媒体构成：视频 / 图片占比 + 各库 media_count 横条（复用 FR-117 summary） */}
      <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md">
        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="xs">媒体构成</Text>
          {composeTotal === 0 ? (
            <Text size="sm" c="dimmed">暂无媒体。</Text>
          ) : (
            <Stack gap="xs">
              <Progress.Root size="xl">
                <Progress.Section value={videoPct} color="purple" aria-label={`视频占比 ${videoPct}%`}>
                  <Progress.Label>视频 {videoPct}%</Progress.Label>
                </Progress.Section>
                <Progress.Section value={imagePct} color="grape" aria-label={`图片占比 ${imagePct}%`}>
                  <Progress.Label>图片 {imagePct}%</Progress.Label>
                </Progress.Section>
              </Progress.Root>
              <Group gap="lg">
                <Group gap={6}><Badge color="purple" variant="filled" size="sm">视频</Badge><Text size="sm">{summary.video_count.toLocaleString()}</Text></Group>
                <Group gap={6}><Badge color="grape" variant="filled" size="sm">图片</Badge><Text size="sm">{summary.image_count.toLocaleString()}</Text></Group>
              </Group>
            </Stack>
          )}
        </Card>

        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="xs">各存储库媒体数</Text>
          {summary.by_library.length === 0 ? (
            <Text size="sm" c="dimmed">暂无媒体库。</Text>
          ) : (
            <Stack gap="xs">
              {summary.by_library.map((l) => (
                <div key={l.library_id}>
                  <Group justify="space-between" mb={2}>
                    <Text size="sm" truncate>{l.label || `库 #${l.library_id}`}</Text>
                    <Text size="sm" c="dimmed">{l.media_count.toLocaleString()}</Text>
                  </Group>
                  <Progress value={libMax > 0 ? (l.media_count / libMax) * 100 : 0} color="purple" size="sm" aria-label={`${l.label || `库 #${l.library_id}`} 媒体 ${l.media_count} 个`} />
                </div>
              ))}
            </Stack>
          )}
        </Card>
      </SimpleGrid>
    </Stack>
  )
}
