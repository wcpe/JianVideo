import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MantineProvider } from '@mantine/core'
import TrendChart from './TrendChart'

// Recharts 在 jsdom 下 ResponsiveContainer 量到的宽高为 0、图形不渲染且会告警。
// 这里把 ResponsiveContainer 替换为固定尺寸容器，让内部 LineChart 拿到确定宽高正常渲染，避免告警致测试失败。
vi.mock('recharts', async () => {
  const actual = await vi.importActual<typeof import('recharts')>('recharts')
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
      <div style={{ width: 600, height: 300 }}>{children}</div>
    ),
  }
})

function renderWithProvider(ui: React.ReactElement) {
  return render(<MantineProvider>{ui}</MantineProvider>)
}

const sample = [
  { date: '2026-05-01', count: 1 },
  { date: '2026-05-02', count: 3 },
  { date: '2026-05-03', count: 2 },
]

describe('TrendChart', () => {
  it('有数据时渲染图表容器（不显占位）', () => {
    renderWithProvider(
      <TrendChart data={sample} xKey="date" series={[{ dataKey: 'count', label: '观看数' }]} />,
    )
    // 渲染图表容器（jsdom 下不断言 SVG path 内部，只断言行为：图表渲染而非占位）
    expect(screen.getByTestId('trend-chart')).toBeInTheDocument()
    // 不显「暂无数据」占位
    expect(screen.queryByTestId('trend-empty')).toBeNull()
    expect(screen.queryByText('暂无数据')).toBeNull()
  })

  it('空数据时渲染「暂无数据」占位，不渲染空图', () => {
    renderWithProvider(
      <TrendChart data={[]} xKey="date" series={[{ dataKey: 'count', label: '观看数' }]} />,
    )
    expect(screen.getByTestId('trend-empty')).toBeInTheDocument()
    expect(screen.getByText('暂无数据')).toBeInTheDocument()
  })

  it('多序列数据也渲染图表容器（不报错、不显占位）', () => {
    const multi = [
      { date: '2026-05-01', a: 1, b: 4 },
      { date: '2026-05-02', a: 2, b: 5 },
    ]
    renderWithProvider(
      <TrendChart
        data={multi}
        xKey="date"
        series={[{ dataKey: 'a', label: 'A' }, { dataKey: 'b', label: 'B' }]}
      />,
    )
    // 多序列渲染图表容器而非占位（jsdom 下不断言 SVG path 内部）
    expect(screen.getByTestId('trend-chart')).toBeInTheDocument()
    expect(screen.queryByTestId('trend-empty')).toBeNull()
  })
})
