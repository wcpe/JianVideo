import type { WatchStats } from '@/types'
import client from './client'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── 真实 API 实现 ──────────────────────────────────

async function realGetWatchStats(): Promise<WatchStats> {
  const res = await client.get<WatchStats>('/api/library/stats')
  return res.data
}

// ─── Mock API 实现 ──────────────────────────────────
// mock 模式给一组示例统计，便于离线 demo 展示各维度。

async function mockGetWatchStats(): Promise<WatchStats> {
  await mockDelay(120)
  return {
    total: 42,
    watched: 18,
    unwatched: 24,
    recent_timeline: [
      { date: '2026-06-24', count: 5 },
      { date: '2026-06-23', count: 3 },
      { date: '2026-06-22', count: 8 },
      { date: '2026-06-20', count: 2 },
    ],
    position_heatmap: [3, 1, 0, 2, 1, 4, 0, 1, 2, 6],
    by_library: [
      { library_id: 1, label: '电影', watched: 12 },
      { library_id: 2, label: '剧集', watched: 6 },
    ],
    by_format: [
      { format: 'mp4', watched: 11 },
      { format: 'mkv', watched: 7 },
    ],
    top_viewed: [],
  }
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getWatchStats() { return useMock ? mockGetWatchStats() : realGetWatchStats() }
