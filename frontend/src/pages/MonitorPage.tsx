import { useState, useEffect } from 'react';
import { Stack, Text, Card, SegmentedControl, SimpleGrid, Loader, Center } from '@mantine/core';
import MetricCard from '@/components/MetricCard';
import PageHeader from '@/components/PageHeader';
import TrendChart from '@/components/TrendChart';
import { getSystemMetrics } from '@/api/metrics';
import { formatBytes } from '@/utils/format';
import type { SystemMetrics, MetricPoint } from '@/types';

// range 选项（FR-119）：与后端 GET /api/system/metrics?range= 取值一致；默认 24h。
const RANGE_1H = '1h';
const RANGE_24H = '24h';
const RANGE_7D = '7d';
const DEFAULT_RANGE = RANGE_24H;
const RANGE_OPTIONS = [
  { value: RANGE_1H, label: '近 1 小时' },
  { value: RANGE_24H, label: '近 24 小时' },
  { value: RANGE_7D, label: '近 7 天' },
];

// 进入页轮询周期（毫秒，FR-119）：每 15s 重拉当前 range，与后端采样周期对齐。
const POLL_INTERVAL_MS = 15000;

/** 磁盘已用百分比（0-100 取整）：总量为 0 时返回 0，避免除零 */
function diskPercent(used: number, total: number): number {
  return total > 0 ? Math.round((used / total) * 100) : 0;
}

/**
 * 系统监控页（FR-119）：CPU / 内存 / 磁盘 / 转码并发的当前值卡 + 时序折线。
 * - range（1h/24h/7d）经 SegmentedControl 切换，默认 24h；切换即重拉。
 * - 进入页每 15s 轮询重拉当前 range，离开（卸载）停轮询；请求失败静默降级（不弹 toast）。
 * - 无样本（current==null 或 points 为空）显占位「暂无采样数据，稍候…」不报错。
 * - 复用 FR-118 的 MetricCard / TrendChart 与 utils/format 的 formatBytes，懒加载落 recharts 到本页 chunk。
 */
export default function MonitorPage() {
  const [range, setRange] = useState<string>(DEFAULT_RANGE);
  const [metrics, setMetrics] = useState<SystemMetrics | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    // 切 range 时先回到加载态，避免短暂展示上一个 range 的数据
    setLoading(true);

    // 拉取一次当前 range；失败静默置空（沿用 silent 轮询惯例，不弹 toast 堆积）
    const fetchOnce = () => {
      getSystemMetrics(range)
        .then((m) => {
          if (active) setMetrics(m);
        })
        .catch(() => {
          /* 轮询失败本就静默重试，不弹 toast；保留上次数据不强制清空 */
        })
        .finally(() => {
          if (active) setLoading(false);
        });
    };

    fetchOnce();
    // 进入页轮询：每 15s 重拉当前 range；卸载或切 range 时清理定时器停轮询
    const timer = window.setInterval(fetchOnce, POLL_INTERVAL_MS);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [range]);

  const current = metrics?.current ?? null;
  const points = metrics?.points ?? [];

  return (
    <Stack gap="lg">
      <PageHeader
        title="系统监控"
        actions={
          <SegmentedControl
            value={range}
            onChange={setRange}
            data={RANGE_OPTIONS}
            aria-label="选择时间范围"
          />
        }
      />
      <Text size="sm" c="dimmed">
        CPU / 内存 / 磁盘 / 转码并发的当前值与随时间的变动（每 15 秒自动刷新）。
      </Text>

      {loading && !metrics ? (
        <Center py="xl">
          <Loader color="purple" />
        </Center>
      ) : !current || points.length === 0 ? (
        // 刚启动无样本：显占位不报错（采样器尚未写入任何样本）
        <Card withBorder padding="xl" radius="md">
          <Center>
            <Text c="dimmed">暂无采样数据，稍候…</Text>
          </Center>
        </Card>
      ) : (
        <MonitorContent current={current} points={points} />
      )}
    </Stack>
  );
}

/** 监控内容区：有样本时渲染当前值卡 + 时序折线；current 与 points 均已非空 */
function MonitorContent({ current, points }: { current: MetricPoint; points: MetricPoint[] }) {
  // 各指标 sparkline 序列（升序，取对应 points 字段）；MetricCard 自动据末两点显涨跌
  const cpuSpark = points.map((p) => p.cpu_percent);
  const memSpark = points.map((p) => p.mem_used_bytes);
  const diskSpark = points.map((p) => p.disk_used_bytes);
  const transcodeSpark = points.map((p) => p.transcode_active);
  // 磁盘已用百分比（当前值卡副标题）
  const diskPct = diskPercent(current.disk_used_bytes, current.disk_total_bytes);

  return (
    <Stack gap="lg">
      {/* 当前值卡：CPU% / 内存 / 磁盘（已用/总量 + 百分比）/ 转码并发 */}
      <SimpleGrid cols={{ base: 1, sm: 2, md: 4 }} spacing="md">
        <MetricCard
          label="CPU 使用率"
          value={`${current.cpu_percent.toFixed(1)}%`}
          sparkline={cpuSpark}
        />
        <MetricCard
          label="内存（进程已用）"
          value={formatBytes(current.mem_used_bytes)}
          sparkline={memSpark}
        />
        <MetricCard
          label={`磁盘（已用 ${diskPct}%）`}
          value={`${formatBytes(current.disk_used_bytes)} / ${formatBytes(current.disk_total_bytes)}`}
          sparkline={diskSpark}
        />
        <MetricCard
          label="转码并发"
          value={current.transcode_active.toLocaleString()}
          sparkline={transcodeSpark}
        />
      </SimpleGrid>

      {/* CPU 使用率时序折线（x=points[].t，可 hover 看每点精确值） */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">
          CPU 使用率趋势
        </Text>
        <Text size="xs" c="dimmed" mb="sm">
          系统 CPU 使用率随时间的变动（%）。
        </Text>
        <TrendChart
          data={points as unknown as Array<Record<string, unknown>>}
          xKey="t"
          series={[{ dataKey: 'cpu_percent', label: 'CPU %' }]}
          valueFormatter={(v) => `${v.toFixed(1)}%`}
        />
      </Card>

      {/* 内存时序折线（进程已用，字节→人类可读） */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">
          内存趋势
        </Text>
        <Text size="xs" c="dimmed" mb="sm">
          进程已用内存随时间的变动。
        </Text>
        <TrendChart
          data={points as unknown as Array<Record<string, unknown>>}
          xKey="t"
          series={[{ dataKey: 'mem_used_bytes', label: '内存' }]}
          valueFormatter={formatBytes}
        />
      </Card>

      {/* 转码并发时序折线（活跃会话数） */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="xs">
          转码并发趋势
        </Text>
        <Text size="xs" c="dimmed" mb="sm">
          活跃转码 / 播放会话数随时间的变动。
        </Text>
        <TrendChart
          data={points as unknown as Array<Record<string, unknown>>}
          xKey="t"
          series={[{ dataKey: 'transcode_active', label: '并发数' }]}
        />
      </Card>
    </Stack>
  );
}
