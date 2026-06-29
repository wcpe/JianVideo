import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts';

// 品牌紫锚点与次要色：用 Mantine CSS 变量（运行时随主题/亮暗解析），不写死颜色、不依赖 JS 主题对象
const BRAND_PURPLE = 'var(--mantine-color-purple-6)';
const AXIS_COLOR = 'var(--mantine-color-dimmed)';
const GRID_COLOR = 'var(--mantine-color-default-border)';

/** 一条折线序列的配置：取值字段、图例标签与线色（缺省走品牌紫） */
export interface TrendSeries {
  /** 数据点中该序列对应的字段名 */
  dataKey: string;
  /** 图例 / Tooltip 显示名 */
  label: string;
  /** 线色，缺省由 TrendChart 取品牌紫 */
  color?: string;
}

interface TrendChartProps {
  /** 折线数据点数组（已按 x 轴升序） */
  data: Array<Record<string, unknown>>;
  /** x 轴字段名（如日期 date） */
  xKey: string;
  /** 一条或多条折线序列 */
  series: TrendSeries[];
  /** 图表高度（像素），缺省 240 */
  height?: number;
  /** Tooltip 数值格式化（如字节→人类可读、秒→小时），缺省原样 */
  valueFormatter?: (value: number) => string;
}

/**
 * 通用时序折线图（FR-118，基于 Recharts）。
 * - 线色默认取品牌紫（theme.colors.purple[6]），可由 series.color 覆盖；
 * - 坐标轴 / 网格线用次要色（dimmed / default-border），保证暗色下可读；
 * - 响应式（ResponsiveContainer）+ 可 hover 看每点精确值（Tooltip）。
 * 数据为空时渲染「暂无数据」占位，不渲染空图。
 */
export default function TrendChart({
  data,
  xKey,
  series,
  height = 240,
  valueFormatter,
}: TrendChartProps) {
  // 空序列：显占位文案，不渲染空图（沿用 FR-88 稀疏态处理）
  if (!data || data.length === 0) {
    return (
      <div
        style={{ height, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
        data-testid="trend-empty"
      >
        <span
          style={{ color: 'var(--mantine-color-dimmed)', fontSize: 'var(--mantine-font-size-sm)' }}
        >
          暂无数据
        </span>
      </div>
    );
  }

  return (
    // 外层固定高度容器：ResponsiveContainer 需要有确定高度的父级才能量宽高
    <div style={{ width: '100%', height }} data-testid="trend-chart">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 8, right: 12, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke={GRID_COLOR} />
          <XAxis
            dataKey={xKey}
            tick={{ fontSize: 12, fill: AXIS_COLOR }}
            stroke={GRID_COLOR}
            minTickGap={24}
          />
          <YAxis tick={{ fontSize: 12, fill: AXIS_COLOR }} stroke={GRID_COLOR} width={48} />
          <Tooltip
            // 适配 recharts Tooltip.formatter 的 ValueType（string|number|...）：仅在传入时把数值经 valueFormatter 转可读字符串
            formatter={valueFormatter ? (value) => valueFormatter(Number(value)) : undefined}
            contentStyle={{
              background: 'var(--mantine-color-body)',
              border: '1px solid var(--mantine-color-default-border)',
              borderRadius: 8,
              fontSize: 12,
            }}
          />
          {series.length > 1 && <Legend wrapperStyle={{ fontSize: 12 }} />}
          {series.map((s) => (
            <Line
              key={s.dataKey}
              type="monotone"
              dataKey={s.dataKey}
              name={s.label}
              stroke={s.color ?? BRAND_PURPLE}
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
