import { describe, expect, it } from 'vitest';
import { mkdir, rm } from 'node:fs/promises';
import path from 'node:path';
import {
  evaluateBackendQueryBenchmark,
  summarizeFrameCost,
  summarizeFrontendBenchmark,
  writeBenchmarkSummary,
} from './index';

describe('benchmark package', () => {
  it('计算帧耗时分位数', () => {
    expect(summarizeFrameCost([1, 2, 3, 4, 5])).toEqual({ p95: 5, p99: 5 });
  });

  it('空样本抛出中文错误', () => {
    expect(() => summarizeFrameCost([])).toThrow('无法计算空样本分位数');
  });

  it('汇总前端帧耗时、长任务与纹理指标并判定达标', () => {
    const report = summarizeFrontendBenchmark({
      dataset: 'media-ui-target-1m',
      environment: { browser: 'vitest', dpr: 1, gpu: 'mock', mode: 'test', viewport: '1280x720' },
      frameCostsMs: [8, 10, 12, 14, 15, 16, 16.5],
      hlsRequests: 1,
      initialInteractiveMs: 2_100,
      jsHeapUsedBytes: 64_000_000,
      longTasksMs: [80, 120],
      pixiObjectCount: 240,
      seed: 'fr2-063',
      textureCount: 80,
      textureMemoryBytes: 20_480_000,
      thumbnailRequests: 160,
    });

    expect(report.frontend.p95).toBe(16.5);
    expect(report.frontend.p99).toBe(16.5);
    expect(report.frontend.pass).toBe(true);
  });

  it('超出 FR2-003 前端门时给出中文原因', () => {
    const report = summarizeFrontendBenchmark({
      dataset: 'media-ui-target-1m',
      environment: { browser: 'vitest', dpr: 1, gpu: 'mock', mode: 'test', viewport: '1280x720' },
      frameCostsMs: [20, 35, 40],
      hlsRequests: 0,
      initialInteractiveMs: 3_500,
      jsHeapUsedBytes: 64_000_000,
      longTasksMs: [250],
      pixiObjectCount: 240,
      seed: 'fr2-063',
      textureCount: 80,
      textureMemoryBytes: 20_480_000,
      thumbnailRequests: 160,
    });

    expect(report.frontend.pass).toBe(false);
    expect(report.frontend.failures).toContain('连续滚动帧耗时 p95 超过 16.7ms');
    expect(report.frontend.failures).toContain('10s 滚动窗口存在超过 200ms 的长任务');
    expect(report.frontend.failures).toContain('初始可交互超过 3s');
  });

  it('按 FR2-003 后端查询门槛判定 10m 查询', () => {
    const ok = evaluateBackendQueryBenchmark({
      dataset: 'media-index-10m',
      p95: 450,
      query: 'space-time-page',
      scannedRows: 1_200,
    });
    const failed = evaluateBackendQueryBenchmark({
      dataset: 'media-index-10m',
      p95: 850,
      query: 'path-prefix',
      scannedRows: 2_000,
    });

    expect(ok.pass).toBe(true);
    expect(failed.pass).toBe(false);
    expect(failed.thresholdMs).toBe(800);
  });

  it('把 benchmark summary 写入可复跑目录', async () => {
    const outputRoot = path.resolve(process.cwd(), '../../.tmp/test-benchmark/fr2-063');
    await rm(outputRoot, { force: true, recursive: true });
    await mkdir(outputRoot, { recursive: true });

    const report = summarizeFrontendBenchmark({
      dataset: 'media-ui-target-1m',
      environment: { browser: 'vitest', dpr: 1, gpu: 'mock', mode: 'test', viewport: '1280x720' },
      frameCostsMs: [8, 10, 12],
      hlsRequests: 1,
      initialInteractiveMs: 1_000,
      jsHeapUsedBytes: 64_000_000,
      longTasksMs: [],
      pixiObjectCount: 72,
      seed: 'fr2-063',
      textureCount: 12,
      textureMemoryBytes: 3_072_000,
      thumbnailRequests: 72,
    });

    const result = await writeBenchmarkSummary(outputRoot, {
      backend: [
        evaluateBackendQueryBenchmark({
          dataset: 'media-index-5m',
          p95: 120,
          query: 'space-time-page',
          scannedRows: 900,
        }),
      ],
      coverageNotes: ['真实 PixiJS 预览画布、Canvas 非空检查与 HLS 预览计数通过'],
      frontend: report.frontend,
      metadata: report.metadata,
    }, new Date('2026-07-08T00:00:00Z'));

    expect(result.filePath).toMatch(/summary\.md$/);
    expect(result.markdown).toContain('FR2-063 Benchmark Summary');
    expect(result.markdown).toContain('真实 PixiJS 预览画布、Canvas 非空检查与 HLS 预览计数通过');
    expect(result.markdown).toContain('p95');
    expect(result.markdown).toContain('media-index-5m');
  });
});
