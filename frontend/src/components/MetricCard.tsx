import { Card, Group, Text } from '@mantine/core'
import { IconArrowUpRight, IconArrowDownRight } from '@tabler/icons-react'
import { ResponsiveContainer, LineChart, Line } from 'recharts'

// 品牌紫锚点：用 Mantine CSS 变量（运行时随主题/亮暗解析），不写死颜色、不依赖 JS 主题对象
const BRAND_PURPLE = 'var(--mantine-color-purple-6)'

interface MetricCardProps {
  /** 指标标题（如「已看」「媒体总数」） */
  label: string
  /** 当前值大数（已格式化的字符串，如 `1,024` / `12.3 GB`） */
  value: string
  /** sparkline 序列（数值数组，末端为最新）；为空 / 过短则不渲染迷你图与涨跌 */
  sparkline?: number[]
  /** 线色，缺省取品牌紫 */
  color?: string
}

/** 由序列末两点推算涨跌：返回 1 上升 / -1 下降 / 0 持平；序列不足 2 点返回 null（不显涨跌） */
function computeDelta(series: number[]): 1 | -1 | 0 | null {
  if (series.length < 2) return null
  const last = series[series.length - 1]
  const prev = series[series.length - 2]
  if (last > prev) return 1
  if (last < prev) return -1
  return 0
}

/**
 * 当前值卡（FR-118）：当前值大数 + 迷你 sparkline（无轴无网格）+ 涨跌箭头。
 * - sparkline 由 series 末端窗口绘制；序列为空 / 过短时退化为不渲染迷你图，不报错（沿用 FR-88）。
 * - 涨跌由序列末两点推算；无序列或不足 2 点则不显示。
 * - 线色走品牌紫主题 token，暗色适配。
 */
export default function MetricCard({ label, value, sparkline, color }: MetricCardProps) {
  const lineColor = color ?? BRAND_PURPLE
  const series = sparkline ?? []
  const hasSpark = series.length >= 2
  const delta = computeDelta(series)
  // sparkline 数据转为 Recharts 需要的对象数组
  const sparkData = series.map((v, i) => ({ i, v }))

  return (
    <Card withBorder padding="md" radius="md">
      <Group justify="space-between" align="flex-start" wrap="nowrap">
        <div style={{ minWidth: 0 }}>
          <Text size="xs" c="dimmed" truncate>{label}</Text>
          <Text fw={700} fz={28} lh={1.1} mt={4}>{value}</Text>
        </div>
        {delta !== null && delta !== 0 && (
          // 涨跌箭头：上升用品牌绿、下降用警示红（语义色 token，暗色适配）
          delta > 0 ? (
            <IconArrowUpRight size={20} style={{ color: 'var(--mantine-color-teal-6)', flexShrink: 0 }} aria-label="较前一点上升" />
          ) : (
            <IconArrowDownRight size={20} style={{ color: 'var(--mantine-color-red-6)', flexShrink: 0 }} aria-label="较前一点下降" />
          )
        )}
      </Group>
      {hasSpark && (
        <div style={{ height: 40, marginTop: 8 }} data-testid="metric-sparkline">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={sparkData} margin={{ top: 2, right: 2, left: 2, bottom: 2 }}>
              <Line type="monotone" dataKey="v" stroke={lineColor} strokeWidth={2} dot={false} isAnimationActive={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}
    </Card>
  )
}
