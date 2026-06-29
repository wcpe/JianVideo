import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { MantineProvider } from '@mantine/core';
import MetricCard from './MetricCard';

// 同 TrendChart：把 ResponsiveContainer 替换为固定尺寸容器，规避 jsdom 下 sparkline 宽高为 0 的告警
vi.mock('recharts', async () => {
  const actual = await vi.importActual<typeof import('recharts')>('recharts');
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
      <div style={{ width: 200, height: 40 }}>{children}</div>
    ),
  };
});

function renderWithProvider(ui: React.ReactElement) {
  return render(<MantineProvider>{ui}</MantineProvider>);
}

describe('MetricCard', () => {
  it('渲染标题与当前值大数', () => {
    renderWithProvider(<MetricCard label="媒体总数" value="1,024" />);
    expect(screen.getByText('媒体总数')).toBeVisible();
    expect(screen.getByText('1,024')).toBeVisible();
  });

  it('无 sparkline 序列时不渲染迷你图与涨跌箭头', () => {
    renderWithProvider(<MetricCard label="未看" value="6" />);
    expect(screen.queryByTestId('metric-sparkline')).toBeNull();
    expect(screen.queryByLabelText('较前一点上升')).toBeNull();
    expect(screen.queryByLabelText('较前一点下降')).toBeNull();
  });

  it('序列末端上升时显示上升箭头与 sparkline', () => {
    renderWithProvider(<MetricCard label="已看" value="4" sparkline={[1, 2, 3]} />);
    expect(screen.getByTestId('metric-sparkline')).toBeInTheDocument();
    expect(screen.getByLabelText('较前一点上升')).toBeInTheDocument();
    expect(screen.queryByLabelText('较前一点下降')).toBeNull();
  });

  it('序列末端下降时显示下降箭头', () => {
    renderWithProvider(<MetricCard label="已看" value="4" sparkline={[5, 4, 2]} />);
    expect(screen.getByLabelText('较前一点下降')).toBeInTheDocument();
    expect(screen.queryByLabelText('较前一点上升')).toBeNull();
  });

  it('序列过短（不足 2 点）时不渲染迷你图与涨跌', () => {
    renderWithProvider(<MetricCard label="追看中" value="1" sparkline={[3]} />);
    expect(screen.queryByTestId('metric-sparkline')).toBeNull();
    expect(screen.queryByLabelText('较前一点上升')).toBeNull();
  });
});
