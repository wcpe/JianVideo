import type { HealthIssue, HealthScanStatus } from '@/types';
import client from './client';

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true';

function mockDelay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

// ─── 健康巡检触发响应（FR-73）──────────────────────────
export interface HealthScanTriggerResponse {
  status: string; // "scanning" / "already_running"
}

// ─── 真实 API 实现 ──────────────────────────────────

async function realStartHealthScan(): Promise<HealthScanTriggerResponse> {
  const res = await client.post<HealthScanTriggerResponse>('/api/library/health/scan');
  return res.data;
}

async function realGetHealthStatus(): Promise<HealthScanStatus> {
  const res = await client.get<HealthScanStatus>('/api/library/health/status');
  return res.data;
}

async function realGetHealthIssues(): Promise<HealthIssue[]> {
  const res = await client.get<{ items: HealthIssue[] }>('/api/library/health/issues');
  return res.data.items;
}

// ─── Mock API 实现 ──────────────────────────────────
// mock 模式下巡检为空操作：状态直接 completed、无问题（巡检需真实 ffprobe/文件系统，离线 demo 无意义）

async function mockStartHealthScan(): Promise<HealthScanTriggerResponse> {
  await mockDelay(200);
  return { status: 'scanning' };
}

async function mockGetHealthStatus(): Promise<HealthScanStatus> {
  await mockDelay(80);
  return {
    status: 'completed',
    total: 0,
    checked: 0,
    issue_count: 0,
    error: '',
    started_at: '',
    completed_at: new Date().toISOString(),
  };
}

async function mockGetHealthIssues(): Promise<HealthIssue[]> {
  await mockDelay(80);
  return [];
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function startHealthScan() {
  return useMock ? mockStartHealthScan() : realStartHealthScan();
}
export function getHealthStatus() {
  return useMock ? mockGetHealthStatus() : realGetHealthStatus();
}
export function getHealthIssues() {
  return useMock ? mockGetHealthIssues() : realGetHealthIssues();
}
