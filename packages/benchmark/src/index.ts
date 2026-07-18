export interface PercentileReport {
  readonly p95: number;
  readonly p99: number;
}

export type BackendBenchmarkDataset =
  "media-index-1m" | "media-index-5m" | "media-index-10m";

export type BackendBenchmarkQuery =
  "filter-combination" | "path-prefix" | "space-time-page" | "task-queue";

export interface FrontendBenchmarkEnvironment {
  readonly browser: string;
  readonly dpr: number;
  readonly gpu: string;
  readonly mode: string;
  readonly viewport: string;
}

export interface FrontendBenchmarkInput {
  readonly dataset: string;
  readonly environment: FrontendBenchmarkEnvironment;
  readonly frameCostsMs: readonly number[];
  readonly hlsRequests: number;
  readonly initialInteractiveMs: number;
  readonly jsHeapUsedBytes: number;
  readonly longTasksMs: readonly number[];
  readonly pixiObjectCount: number;
  readonly seed: string;
  readonly textureCount: number;
  readonly textureMemoryBytes: number;
  readonly thumbnailRequests: number;
}

export interface FrontendBenchmarkSummary {
  readonly dataset: string;
  readonly failures: readonly string[];
  readonly hlsRequests: number;
  readonly initialInteractiveMs: number;
  readonly jsHeapUsedBytes: number;
  readonly longTaskCount: number;
  readonly maxLongTaskMs: number;
  readonly p95: number;
  readonly p99: number;
  readonly pass: boolean;
  readonly pixiObjectCount: number;
  readonly textureCount: number;
  readonly textureMemoryBytes: number;
  readonly thumbnailRequests: number;
}

export interface BenchmarkMetadata {
  readonly dataset: string;
  readonly environment: FrontendBenchmarkEnvironment;
  readonly seed: string;
}

export interface FrontendBenchmarkReport {
  readonly frontend: FrontendBenchmarkSummary;
  readonly metadata: BenchmarkMetadata;
}

export interface BackendQueryBenchmarkInput {
  readonly dataset: BackendBenchmarkDataset;
  readonly p95: number;
  readonly query: BackendBenchmarkQuery;
  readonly scannedRows: number;
}

export interface BackendQueryBenchmarkSummary extends BackendQueryBenchmarkInput {
  readonly pass: boolean;
  readonly thresholdMs: number;
}

export interface BenchmarkSummaryInput {
  readonly backend: readonly BackendQueryBenchmarkSummary[];
  readonly coverageNotes?: readonly string[];
  readonly frontend: FrontendBenchmarkSummary;
  readonly metadata: BenchmarkMetadata;
}

export interface WrittenBenchmarkSummary {
  readonly filePath: string;
  readonly markdown: string;
}

export function percentile(values: readonly number[], ratio: number): number {
  if (values.length === 0) {
    throw new Error("无法计算空样本分位数");
  }
  const sorted = [...values].sort((left, right) => left - right);
  const index = Math.min(
    sorted.length - 1,
    Math.ceil(sorted.length * ratio) - 1,
  );
  return sorted[index] as number;
}

export function summarizeFrameCost(
  values: readonly number[],
): PercentileReport {
  return {
    p95: percentile(values, 0.95),
    p99: percentile(values, 0.99),
  };
}

export function summarizeFrontendBenchmark(
  input: FrontendBenchmarkInput,
): FrontendBenchmarkReport {
  const frameCost = summarizeFrameCost(input.frameCostsMs);
  const maxLongTaskMs =
    input.longTasksMs.length === 0 ? 0 : Math.max(...input.longTasksMs);
  const failures = [
    ...(frameCost.p95 > 16.7 ? ["连续滚动帧耗时 p95 超过 16.7ms"] : []),
    ...(frameCost.p99 > 33 ? ["连续滚动帧耗时 p99 超过 33ms"] : []),
    ...(maxLongTaskMs > 200 ? ["10s 滚动窗口存在超过 200ms 的长任务"] : []),
    ...(input.initialInteractiveMs > 3_000 ? ["初始可交互超过 3s"] : []),
  ];

  return {
    frontend: {
      dataset: input.dataset,
      failures,
      hlsRequests: input.hlsRequests,
      initialInteractiveMs: input.initialInteractiveMs,
      jsHeapUsedBytes: input.jsHeapUsedBytes,
      longTaskCount: input.longTasksMs.length,
      maxLongTaskMs,
      p95: frameCost.p95,
      p99: frameCost.p99,
      pass: failures.length === 0,
      pixiObjectCount: input.pixiObjectCount,
      textureCount: input.textureCount,
      textureMemoryBytes: input.textureMemoryBytes,
      thumbnailRequests: input.thumbnailRequests,
    },
    metadata: {
      dataset: input.dataset,
      environment: input.environment,
      seed: input.seed,
    },
  };
}

export function evaluateBackendQueryBenchmark(
  input: BackendQueryBenchmarkInput,
): BackendQueryBenchmarkSummary {
  const thresholdMs = backendThresholdMs(input.dataset, input.query);
  return {
    ...input,
    pass: input.p95 <= thresholdMs,
    thresholdMs,
  };
}

export async function writeBenchmarkSummary(
  outputRoot: string,
  summary: BenchmarkSummaryInput,
  now = new Date(),
): Promise<WrittenBenchmarkSummary> {
  const { mkdir, writeFile } = await import("node:fs/promises");
  const path = await import("node:path");
  const timestamp = now.toISOString().replace(/[:.]/g, "-");
  const directory = path.join(outputRoot, timestamp);
  const filePath = path.join(directory, "summary.md");
  const markdown = renderBenchmarkSummary(summary, now);
  await mkdir(directory, { recursive: true });
  await writeFile(filePath, markdown, "utf8");
  return { filePath, markdown };
}

export function renderBenchmarkSummary(
  summary: BenchmarkSummaryInput,
  now = new Date(),
): string {
  const backendRows = summary.backend
    .map(
      (item) =>
        `| ${item.dataset} | ${item.query} | ${formatNumber(item.p95)} | ${formatNumber(item.thresholdMs)} | ${formatNumber(item.scannedRows)} | ${item.pass ? "达标" : "未达标"} |`,
    )
    .join("\n");
  const failures =
    summary.frontend.failures.length === 0
      ? "无"
      : summary.frontend.failures.join("；");
  const coverageNotes = summary.coverageNotes ?? [];
  const coverageSection =
    coverageNotes.length === 0
      ? []
      : ["", "## 覆盖说明", "", ...coverageNotes.map((note) => `- ${note}`)];

  return [
    "# FR2-063 Benchmark Summary",
    "",
    `生成时间：${now.toISOString()}`,
    `数据集：${summary.metadata.dataset}`,
    `Seed：${summary.metadata.seed}`,
    `环境：${summary.metadata.environment.browser} / ${summary.metadata.environment.gpu} / DPR ${formatNumber(summary.metadata.environment.dpr)} / ${summary.metadata.environment.viewport} / ${summary.metadata.environment.mode}`,
    "",
    "## 前端",
    "",
    `- p95：${formatNumber(summary.frontend.p95)}ms`,
    `- p99：${formatNumber(summary.frontend.p99)}ms`,
    `- 长任务：${formatNumber(summary.frontend.longTaskCount)} 个，最大 ${formatNumber(summary.frontend.maxLongTaskMs)}ms`,
    `- 初始可交互：${formatNumber(summary.frontend.initialInteractiveMs)}ms`,
    `- Pixi 对象：${formatNumber(summary.frontend.pixiObjectCount)}`,
    `- 纹理：${formatNumber(summary.frontend.textureCount)} 个，估算 ${formatNumber(summary.frontend.textureMemoryBytes)} bytes`,
    `- JS heap：${formatNumber(summary.frontend.jsHeapUsedBytes)} bytes`,
    `- 请求：缩略图 ${formatNumber(summary.frontend.thumbnailRequests)}，HLS ${formatNumber(summary.frontend.hlsRequests)}`,
    `- 判定：${summary.frontend.pass ? "达标" : "未达标"}；失败项：${failures}`,
    "",
    "## 后端查询",
    "",
    "| 数据集 | 查询 | p95(ms) | 门槛(ms) | 扫描行数 | 判定 |",
    "|---|---|---:|---:|---:|---|",
    backendRows,
    ...coverageSection,
    "",
  ].join("\n");
}

function backendThresholdMs(
  dataset: BackendBenchmarkDataset,
  query: BackendBenchmarkQuery,
): number {
  if (query === "task-queue") {
    return dataset === "media-index-10m" ? 300 : 100;
  }
  if (query === "space-time-page") {
    return dataset === "media-index-10m" ? 500 : 200;
  }
  return dataset === "media-index-10m" ? 800 : 300;
}

function formatNumber(value: number): string {
  return value.toString();
}
