import type { SystemMetrics } from '@/types'
import client from './client'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── 真实 API 实现 ──────────────────────────────────

async function realGetSystemMetrics(range: string): Promise<SystemMetrics> {
  // 后台轮询类请求：标 silent，避免持续失败时网络 toast 无上限堆积（沿用 client.ts 约定）
  const res = await client.get<SystemMetrics>('/api/system/metrics', { params: { range }, silent: true })
  return res.data
}

// ─── Mock API 实现 ──────────────────────────────────
// mock 模式给一组多点示例时序 + current 快照，便于离线 demo 展示监控折线与当前值卡。

async function mockGetSystemMetrics(range: string): Promise<SystemMetrics> {
  await mockDelay(120)
  return {
    range,
    points: [
      { t: '2026-06-27T10:00:00Z', cpu_percent: 32.5, mem_used_bytes: 180000000, mem_sys_bytes: 520000000, disk_used_bytes: 1979900000000, disk_total_bytes: 2600000000000, transcode_active: 1, goroutines: 110 },
      { t: '2026-06-27T11:00:00Z', cpu_percent: 41.0, mem_used_bytes: 195000000, mem_sys_bytes: 536870912, disk_used_bytes: 1980100000000, disk_total_bytes: 2600000000000, transcode_active: 2, goroutines: 118 },
      { t: '2026-06-27T12:00:00Z', cpu_percent: 28.3, mem_used_bytes: 188000000, mem_sys_bytes: 530000000, disk_used_bytes: 1980300000000, disk_total_bytes: 2600000000000, transcode_active: 0, goroutines: 112 },
      { t: '2026-06-27T13:00:00Z', cpu_percent: 55.7, mem_used_bytes: 210000000, mem_sys_bytes: 545000000, disk_used_bytes: 1980600000000, disk_total_bytes: 2600000000000, transcode_active: 3, goroutines: 130 },
      { t: '2026-06-27T14:00:00Z', cpu_percent: 47.2, mem_used_bytes: 202000000, mem_sys_bytes: 540000000, disk_used_bytes: 1980900000000, disk_total_bytes: 2600000000000, transcode_active: 2, goroutines: 122 },
    ],
    current: { t: '2026-06-27T14:00:00Z', cpu_percent: 47.2, mem_used_bytes: 202000000, mem_sys_bytes: 540000000, disk_used_bytes: 1980900000000, disk_total_bytes: 2600000000000, transcode_active: 2, goroutines: 122 },
  }
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getSystemMetrics(range: string): Promise<SystemMetrics> {
  return useMock ? mockGetSystemMetrics(range) : realGetSystemMetrics(range)
}
