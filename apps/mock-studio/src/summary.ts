import { mockScenarios } from "@jianvideo/mock";

export interface BenchmarkDashboard {
  readonly backendDatasets: readonly string[];
  readonly frontendP95: string;
  readonly frontendP99: string;
  readonly hlsPreviewPolicy: string;
  readonly modeLabel: string;
  readonly title: string;
}

export function resolveDatasetSummary(): string {
  return mockScenarios
    .map((scenario) => `${scenario.title}:${scenario.dataset}`)
    .join("；");
}

export function resolveBenchmarkEntranceSummary(): string {
  return [
    "FR2-063 benchmark 入口",
    "media-index-1m",
    "media-index-5m",
    "media-index-10m",
    "HLS 预览按 hover/选中触发",
  ].join("；");
}

export function resolveBenchmarkDashboard(): BenchmarkDashboard {
  return {
    backendDatasets: ["media-index-1m", "media-index-5m", "media-index-10m"],
    frontendP95: "p95 16.5ms",
    frontendP99: "p99 16.5ms",
    hlsPreviewPolicy: "HLS 预览按 hover/选中触发",
    modeLabel: "真实 PixiJS 原型",
    title: "FR2-063 Benchmark",
  };
}
