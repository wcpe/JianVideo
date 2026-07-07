export interface PercentileReport {
  readonly p95: number;
  readonly p99: number;
}

export function percentile(values: readonly number[], ratio: number): number {
  if (values.length === 0) {
    throw new Error('无法计算空样本分位数');
  }
  const sorted = [...values].sort((left, right) => left - right);
  const index = Math.min(sorted.length - 1, Math.ceil(sorted.length * ratio) - 1);
  return sorted[index] as number;
}

export function summarizeFrameCost(values: readonly number[]): PercentileReport {
  return {
    p95: percentile(values, 0.95),
    p99: percentile(values, 0.99),
  };
}
