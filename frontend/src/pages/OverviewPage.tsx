import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { Stack, Text, Group, Card, Progress, SimpleGrid, Badge, Box } from '@mantine/core';
import {
  IconPhoto,
  IconVideo,
  IconClock,
  IconDatabase,
  IconLibrary,
  IconAlbum,
} from '@tabler/icons-react';
import { getLibrarySummary } from '@/api/summary';
import { getWatchStats } from '@/api/stats';
import { getSystemInfo } from '@/api/system';
import { getHealthStatus } from '@/api/health';
import { listTranscodeTasks } from '@/api/transcode';
import { getScanTasks } from '@/api/library';
import { listAlbums } from '@/api/albums';
import PageHeader from '@/components/PageHeader';
import { formatBytes, formatUptime } from '@/utils/format';
import ContinueWatching from '@/components/ContinueWatching';
import type { LibrarySummary, WatchStats, SystemInfo, ScanTask } from '@/types';

// 转码任务计数聚合（FR-117 任务队列分区）：前端按状态聚合 pending/running/completed 三档
interface TranscodeCounts {
  pending: number;
  running: number;
  completed: number;
}

// 把秒数（视频总时长）格式化为「X 小时」整数展示（FR-117 KPI 大数卡）
function formatHours(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '0';
  return Math.round(seconds / 3600).toLocaleString();
}

/**
 * KPI 大数卡（FR-117）：图标 + 大数值 + 标题，可选副标题。
 * 各卡统一品牌紫图标底、token 圆角，复用主题色板。
 */
function KpiCard({
  icon,
  value,
  title,
  subtitle,
}: {
  icon: React.ReactNode;
  value: React.ReactNode;
  title: string;
  subtitle?: string;
}) {
  return (
    <Card withBorder padding="md" radius="md">
      <Group gap="sm" wrap="nowrap" align="center">
        <Box
          style={{
            width: 40,
            height: 40,
            flexShrink: 0,
            borderRadius: 'var(--mantine-radius-md)',
            backgroundColor: 'var(--mantine-color-purple-light)',
            color: 'var(--mantine-color-purple-light-color)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          {icon}
        </Box>
        <Box style={{ minWidth: 0 }}>
          <Text size="xl" fw={700} lh={1.1}>
            {value}
          </Text>
          <Text size="xs" c="dimmed">
            {title}
          </Text>
          {subtitle && (
            <Text size="xs" c="dimmed">
              {subtitle}
            </Text>
          )}
        </Box>
      </Group>
    </Card>
  );
}

/**
 * 环形进度（FR-117 观看概览）：SVG stroke 圆环，不引图表库。
 * percent 为 0-100，中心叠加百分比文字。
 */
function RingProgress({ percent, label }: { percent: number; label: string }) {
  const size = 120;
  const stroke = 12;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const clamped = Math.max(0, Math.min(100, percent));
  const offset = circumference * (1 - clamped / 100);
  return (
    <Box
      role="img"
      aria-label={label}
      style={{ width: size, height: size, position: 'relative', flexShrink: 0 }}
    >
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        {/* 底环：浅色轨道 */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="var(--mantine-color-default-border)"
          strokeWidth={stroke}
        />
        {/* 进度环：品牌紫，从 12 点方向起绕 */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="var(--mantine-color-purple-6)"
          strokeWidth={stroke}
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          strokeLinecap="round"
          transform={`rotate(-90 ${size / 2} ${size / 2})`}
        />
      </svg>
      <Box
        style={{
          position: 'absolute',
          inset: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Text size="lg" fw={700}>
          {clamped}%
        </Text>
      </Box>
    </Box>
  );
}

/**
 * 首页概览数据看板（FR-117）。
 *
 * A 版「分析大盘」布局：自上而下为 KPI 大数卡、媒体构成与各库分布、观看概览、
 * 系统状态、任务队列、继续观看六个分区。各数据源独立 try/catch 降级——
 * 单个数据源失败仅该卡显占位/零值，不整页崩；空库（summary.total===0）仍渲染零值（AC-21）。
 */
export default function OverviewPage() {
  const [summary, setSummary] = useState<LibrarySummary | null>(null);
  const [albumCount, setAlbumCount] = useState<number | null>(null);
  const [watch, setWatch] = useState<WatchStats | null>(null);
  const [system, setSystem] = useState<SystemInfo | null>(null);
  const [issueCount, setIssueCount] = useState<number | null>(null);
  const [scanCurrent, setScanCurrent] = useState<ScanTask | null>(null);
  const [transcode, setTranscode] = useState<TranscodeCounts | null>(null);

  useEffect(() => {
    let active = true;

    // 各数据源独立加载、独立降级：任一失败仅置该卡为占位/零值，不影响其余分区。
    getLibrarySummary()
      .then((s) => {
        if (active) setSummary(s);
      })
      .catch(() => {
        if (active) setSummary(null);
      });
    listAlbums()
      .then((items) => {
        if (active) setAlbumCount(items.length);
      })
      .catch(() => {
        if (active) setAlbumCount(null);
      });
    getWatchStats()
      .then((s) => {
        if (active) setWatch(s);
      })
      .catch(() => {
        if (active) setWatch(null);
      });
    getSystemInfo()
      .then((info) => {
        if (active) setSystem(info);
      })
      .catch(() => {
        if (active) setSystem(null);
      });
    getHealthStatus()
      .then((st) => {
        if (active) setIssueCount(st.issue_count);
      })
      .catch(() => {
        if (active) setIssueCount(null);
      });
    getScanTasks()
      .then((res) => {
        if (active) setScanCurrent(res.current);
      })
      .catch(() => {
        if (active) setScanCurrent(null);
      });
    listTranscodeTasks()
      .then((tasks) => {
        if (!active) return;
        // 前端聚合 pending/running/completed 三档计数（其余状态如 error 不计入这三档）
        const counts: TranscodeCounts = { pending: 0, running: 0, completed: 0 };
        for (const t of tasks) {
          if (t.status === 'pending') counts.pending++;
          else if (t.status === 'running') counts.running++;
          else if (t.status === 'completed') counts.completed++;
        }
        setTranscode(counts);
      })
      .catch(() => {
        if (active) setTranscode(null);
      });

    return () => {
      active = false;
    };
  }, []);

  // 媒体构成占比（视频/图片）：以总数为分母，空库或加载失败时分母为 0、占比为 0。
  const total = summary?.total ?? 0;
  const videoCount = summary?.video_count ?? 0;
  const imageCount = summary?.image_count ?? 0;
  const videoPercent = total > 0 ? Math.round((videoCount / total) * 100) : 0;
  const imagePercent = total > 0 ? Math.round((imageCount / total) * 100) : 0;
  // 各库横条以最大 media_count 为满格基准
  const libraries = summary?.by_library ?? [];
  const libraryMax = Math.max(0, ...libraries.map((l) => l.media_count));

  // 观看概览：已看占比（环形）
  const watchTotal = watch?.total ?? 0;
  const watched = watch?.watched ?? 0;
  const unwatched = watch?.unwatched ?? 0;
  const watchedPercent = watchTotal > 0 ? Math.round((watched / watchTotal) * 100) : 0;

  return (
    <Stack gap="lg">
      <PageHeader title="概览" />

      {/* 1. KPI 大数卡（FR-117）：媒体总数 / 视频总时长 / 占用空间 / 媒体库数 / 相册数 */}
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 5 }} spacing="md">
        <KpiCard
          icon={<IconVideo size={22} />}
          value={total.toLocaleString()}
          title="媒体总数"
          subtitle={`视频 ${videoCount.toLocaleString()} · 图片 ${imageCount.toLocaleString()}`}
        />
        <KpiCard
          icon={<IconClock size={22} />}
          value={formatHours(summary?.total_duration ?? 0)}
          title="视频总时长（小时）"
        />
        <KpiCard
          icon={<IconDatabase size={22} />}
          value={formatBytes(summary?.total_size ?? 0)}
          title="占用空间"
        />
        <KpiCard
          icon={<IconLibrary size={22} />}
          value={(summary?.library_count ?? 0).toLocaleString()}
          title="媒体库数"
        />
        <KpiCard
          icon={<IconAlbum size={22} />}
          value={albumCount === null ? '—' : albumCount.toLocaleString()}
          title="相册数"
        />
      </SimpleGrid>

      {/* 2. 媒体构成与各库分布（FR-117）：视频/图片占比条 + 各库 media_count 横条 */}
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="sm">
            媒体构成
          </Text>
          <Stack gap="md">
            <div>
              <Group justify="space-between" mb={4}>
                <Group gap={6}>
                  <IconVideo size={16} />
                  <Text size="sm">视频</Text>
                </Group>
                <Text size="sm" c="dimmed">
                  {videoCount.toLocaleString()}（{videoPercent}%）
                </Text>
              </Group>
              <Progress
                value={videoPercent}
                color="purple"
                size="lg"
                aria-label={`视频占比 ${videoPercent}%`}
              />
            </div>
            <div>
              <Group justify="space-between" mb={4}>
                <Group gap={6}>
                  <IconPhoto size={16} />
                  <Text size="sm">图片</Text>
                </Group>
                <Text size="sm" c="dimmed">
                  {imageCount.toLocaleString()}（{imagePercent}%）
                </Text>
              </Group>
              <Progress
                value={imagePercent}
                color="grape"
                size="lg"
                aria-label={`图片占比 ${imagePercent}%`}
              />
            </div>
          </Stack>
        </Card>

        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="sm">
            各库分布
          </Text>
          {libraries.length === 0 ? (
            <Text size="sm" c="dimmed">
              暂无媒体库数据。
            </Text>
          ) : (
            <Stack gap="xs">
              {libraries.map((l) => (
                <div key={l.library_id}>
                  <Group justify="space-between" mb={2}>
                    <Text size="sm" truncate>
                      {l.label || `库 #${l.library_id}`}
                    </Text>
                    <Text size="sm" c="dimmed">
                      {l.media_count.toLocaleString()}
                    </Text>
                  </Group>
                  <Progress
                    value={libraryMax > 0 ? (l.media_count / libraryMax) * 100 : 0}
                    color="purple"
                    size="sm"
                    aria-label={`${l.label || `库 #${l.library_id}`} 媒体 ${l.media_count}`}
                  />
                </div>
              ))}
            </Stack>
          )}
        </Card>
      </SimpleGrid>

      {/* 3. 观看概览 + 4. 系统状态（并排两卡） */}
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md">
        <Card withBorder padding="md" radius="md">
          <Group justify="space-between" mb="sm">
            <Text fw={600}>观看概览</Text>
            {/* 跳转观看统计全量页（FR-75） */}
            <Link to="/stats" style={{ textDecoration: 'none' }}>
              <Text size="sm" c="purple">
                查看全部 ›
              </Text>
            </Link>
          </Group>
          <Group gap="lg" align="center" wrap="nowrap">
            <RingProgress percent={watchedPercent} label={`已看占比 ${watchedPercent}%`} />
            <Stack gap="xs">
              <Group gap={6}>
                <Badge color="purple" variant="filled" size="sm">
                  已看
                </Badge>
                <Text size="sm">{watched.toLocaleString()}</Text>
              </Group>
              <Group gap={6}>
                <Badge
                  color="gray"
                  variant="light"
                  size="sm"
                  styles={{ label: { color: 'var(--mantine-color-gray-7)' } }}
                >
                  未看
                </Badge>
                <Text size="sm">{unwatched.toLocaleString()}</Text>
              </Group>
              <Text size="xs" c="dimmed">
                共 {watchTotal.toLocaleString()} 个媒体
              </Text>
            </Stack>
          </Group>
        </Card>

        <Card withBorder padding="md" radius="md">
          <Text fw={600} mb="sm">
            系统状态
          </Text>
          {system ? (
            <Stack gap="xs">
              <Group justify="space-between">
                <Text size="sm" c="dimmed">
                  FFmpeg
                </Text>
                <Badge color={system.ffmpeg.available ? 'green' : 'red'} size="sm">
                  {system.ffmpeg.available ? '可用' : '不可用'}
                </Badge>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">
                  硬件加速
                </Text>
                <Text size="sm">{system.hwaccel.preferred || '软件编码'}</Text>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">
                  运行时长
                </Text>
                <Text size="sm">
                  {system.runtime ? formatUptime(system.runtime.uptime_seconds) : '—'}
                </Text>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">
                  运行内存
                </Text>
                <Text size="sm">
                  {system.runtime ? formatBytes(system.runtime.mem_alloc) : '—'}
                </Text>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">
                  巡检问题
                </Text>
                {issueCount === null ? (
                  <Text size="sm" c="dimmed">
                    —
                  </Text>
                ) : (
                  <Badge color={issueCount > 0 ? 'orange' : 'green'} size="sm">
                    {issueCount} 个
                  </Badge>
                )}
              </Group>
            </Stack>
          ) : (
            <Text size="sm" c="dimmed">
              系统信息加载失败。
            </Text>
          )}
        </Card>
      </SimpleGrid>

      {/* 5. 任务队列（FR-117）：扫描进行中进度 + 转码 pending/running/completed 计数 */}
      <Card withBorder padding="md" radius="md">
        <Text fw={600} mb="sm">
          任务队列
        </Text>
        <Stack gap="md">
          <div>
            <Text size="sm" c="dimmed" mb={4}>
              扫描任务
            </Text>
            {scanCurrent ? (
              <div>
                <Group justify="space-between" mb={2}>
                  <Text size="sm">库 #{scanCurrent.library_id} 扫描中</Text>
                  <Text size="sm" c="dimmed">
                    {scanCurrent.scanned_files} / {scanCurrent.total_files}
                  </Text>
                </Group>
                <Progress
                  value={
                    scanCurrent.total_files > 0
                      ? (scanCurrent.scanned_files / scanCurrent.total_files) * 100
                      : 0
                  }
                  color="purple"
                  size="sm"
                  animated
                  aria-label={`扫描进度 ${scanCurrent.scanned_files} / ${scanCurrent.total_files}`}
                />
              </div>
            ) : (
              <Text size="sm">当前无进行中的扫描任务。</Text>
            )}
          </div>
          <div>
            <Text size="sm" c="dimmed" mb={4}>
              转码任务
            </Text>
            <Group gap="lg">
              <Group gap={6}>
                <Badge color="gray" variant="light" size="sm">
                  待处理
                </Badge>
                <Text size="sm">{transcode?.pending ?? 0}</Text>
              </Group>
              <Group gap={6}>
                <Badge color="blue" variant="light" size="sm">
                  进行中
                </Badge>
                <Text size="sm">{transcode?.running ?? 0}</Text>
              </Group>
              <Group gap={6}>
                <Badge color="green" variant="light" size="sm">
                  已完成
                </Badge>
                <Text size="sm">{transcode?.completed ?? 0}</Text>
              </Group>
            </Group>
          </div>
        </Stack>
      </Card>

      {/* 6. 继续观看（FR-44）：复用现有组件，列表为空时整块不渲染 */}
      <ContinueWatching />
    </Stack>
  );
}
