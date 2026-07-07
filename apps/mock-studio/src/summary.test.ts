import { describe, expect, it } from 'vitest';
import { resolveBenchmarkDashboard, resolveBenchmarkEntranceSummary, resolveDatasetSummary } from './summary';

describe('mock studio summary', () => {
  it('汇总共享 mock 数据集', () => {
    expect(resolveDatasetSummary()).toContain('百万素材压力场景:target-1m');
  });

  it('汇总 FR2-063 benchmark 入口状态', () => {
    const summary = resolveBenchmarkEntranceSummary();

    expect(summary).toContain('FR2-063');
    expect(summary).toContain('media-index-1m');
    expect(summary).toContain('media-index-5m');
    expect(summary).toContain('media-index-10m');
    expect(summary).toContain('HLS 预览按 hover/选中触发');
  });

  it('提供页面可展示的 benchmark 指标', () => {
    const dashboard = resolveBenchmarkDashboard();

    expect(dashboard.title).toBe('FR2-063 Benchmark');
    expect(dashboard.modeLabel).toBe('真实 PixiJS 原型');
    expect(dashboard.frontendP95).toContain('p95');
    expect(dashboard.frontendP99).toContain('p99');
    expect(dashboard.backendDatasets).toEqual(['media-index-1m', 'media-index-5m', 'media-index-10m']);
  });
});
