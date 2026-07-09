import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import MonitorPage from './MonitorPage';
import { server } from '@/mocks/beforeAll';
import type { SystemMetrics } from '@/types';

// Recharts 在 jsdom 下 ResponsiveContainer 量到宽高为 0、不渲染图形且告警；
// 替换为固定尺寸容器，保证监控页内折线/sparkline 正常渲染、无未捕获告警致测试失败。
vi.mock('recharts', async () => {
  const actual = await vi.importActual<typeof import('recharts')>('recharts');
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
      <div style={{ width: 600, height: 300 }}>{children}</div>
    ),
  };
});

const sampleMetrics: SystemMetrics = {
  range: '24h',
  points: [
    {
      t: '2026-06-27T10:00:00Z',
      cpu_percent: 32.5,
      mem_used_bytes: 180000000,
      mem_sys_bytes: 520000000,
      disk_used_bytes: 1979900000000,
      disk_total_bytes: 2600000000000,
      transcode_active: 1,
      goroutines: 110,
    },
    {
      t: '2026-06-27T11:00:00Z',
      cpu_percent: 41.0,
      mem_used_bytes: 195000000,
      mem_sys_bytes: 536870912,
      disk_used_bytes: 1980100000000,
      disk_total_bytes: 2600000000000,
      transcode_active: 2,
      goroutines: 118,
    },
    {
      t: '2026-06-27T12:00:00Z',
      cpu_percent: 28.3,
      mem_used_bytes: 188000000,
      mem_sys_bytes: 530000000,
      disk_used_bytes: 1980300000000,
      disk_total_bytes: 2600000000000,
      transcode_active: 0,
      goroutines: 112,
    },
  ],
  current: {
    t: '2026-06-27T12:00:00Z',
    cpu_percent: 47.2,
    mem_used_bytes: 202000000,
    mem_sys_bytes: 540000000,
    disk_used_bytes: 1980900000000,
    disk_total_bytes: 2600000000000,
    transcode_active: 3,
    goroutines: 122,
  },
};

// 空样本（刚启动）：points 空、current 为 null
const emptyMetrics: SystemMetrics = { range: '24h', points: [], current: null };

function useMetricsHandler(body: SystemMetrics = sampleMetrics) {
  server.use(http.get('*/api/system/metrics', () => HttpResponse.json(body)));
}

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/monitor']}>
        <MonitorPage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('MonitorPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // 基线：给默认 handler，避免未匹配请求告警；各用例可再 server.use 覆盖
    useMetricsHandler();
  });

  it('渲染标题与四张当前值卡（CPU/内存/磁盘/转码并发）', async () => {
    useMetricsHandler();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('系统监控')).toBeVisible();
    });
    // 四张当前值卡标题
    expect(await screen.findByText('CPU 使用率')).toBeVisible();
    expect(screen.getByText('内存（进程已用）')).toBeVisible();
    expect(screen.getByText(/磁盘（已用 \d+%）/)).toBeVisible();
    expect(screen.getByText('转码并发')).toBeVisible();
  });

  it('当前值卡展示 current 快照值（CPU% / 转码并发取自 current 而非时序末点）', async () => {
    useMetricsHandler();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('CPU 使用率')).toBeVisible();
    });
    // current.cpu_percent=47.2 → 「47.2%」；与时序末点（28.3）不同，验证取的是 current
    expect(screen.getByText('47.2%')).toBeVisible();
    // 转码并发取 current.transcode_active=3，定位到该卡内的大数
    const transcodeCard = screen.getByText('转码并发').closest('.mantine-Card-root') as HTMLElement;
    expect(transcodeCard).toBeTruthy();
    expect(transcodeCard.textContent).toContain('3');
  });

  it('渲染三张时序折线卡（CPU / 内存 / 转码并发趋势）', async () => {
    useMetricsHandler();
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('CPU 使用率趋势')).toBeVisible();
    });
    expect(screen.getByText('内存趋势')).toBeVisible();
    expect(screen.getByText('转码并发趋势')).toBeVisible();
    // 有样本时渲染图表容器（非「暂无数据」占位）
    expect(screen.getAllByTestId('trend-chart').length).toBe(3);
  });

  it('提供 range 选择（默认 24h），切到 1h 重拉并仍渲染当前值卡', async () => {
    const user = userEvent.setup();
    // 记录每次请求带的 range 参数，验证切换触发以新 range 重拉
    const seenRanges: string[] = [];
    server.use(
      http.get('*/api/system/metrics', ({ request }) => {
        seenRanges.push(new URL(request.url).searchParams.get('range') ?? '');
        return HttpResponse.json(sampleMetrics);
      }),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('CPU 使用率')).toBeVisible();
    });
    // 默认以 24h 拉取
    expect(seenRanges).toContain('24h');

    // 切到「近 1 小时」：SegmentedControl 以 radio 呈现，按可访问名点击
    await user.click(screen.getByRole('radio', { name: '近 1 小时' }));
    await waitFor(() => {
      expect(seenRanges).toContain('1h');
    });
    // 切换后仍渲染当前值卡
    expect(screen.getByText('CPU 使用率')).toBeVisible();
  });

  it('无样本（current=null / points 空）显占位「暂无采样数据」不报错', async () => {
    server.use(http.get('*/api/system/metrics', () => HttpResponse.json(emptyMetrics)));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/暂无采样数据/)).toBeVisible();
    });
    // 占位时不渲染当前值卡与图表
    expect(screen.queryByText('CPU 使用率趋势')).toBeNull();
    expect(screen.queryByTestId('trend-chart')).toBeNull();
  });

  it('请求失败时静默降级：不抛错、显占位（保留加载兜底而非崩溃）', async () => {
    server.use(
      http.get('*/api/system/metrics', () =>
        HttpResponse.json({ message: '炸了' }, { status: 500 }),
      ),
    );
    renderPage();
    // 失败后 loading 结束、metrics 仍为 null → 走占位分支，不报错
    await waitFor(() => {
      expect(screen.getByText(/暂无采样数据/)).toBeVisible();
    });
  });
});
