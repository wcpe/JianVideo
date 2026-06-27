import type { MediaTrends } from '@/types'
import client from './client'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── 真实 API 实现 ──────────────────────────────────

async function realGetMediaTrends(): Promise<MediaTrends> {
  const res = await client.get<MediaTrends>('/api/library/trends')
  return res.data
}

// ─── Mock API 实现 ──────────────────────────────────
// mock 模式给一组多日示例，便于离线 demo 展示媒体增长 / 累计容量 / 时长曲线。

async function mockGetMediaTrends(): Promise<MediaTrends> {
  await mockDelay(120)
  return {
    media_added: [
      { date: '2026-05-01', count: 10, size: 1000000000, duration: 3000 },
      { date: '2026-05-03', count: 5, size: 500000000, duration: 1500 },
      { date: '2026-05-08', count: 8, size: 820000000, duration: 2400 },
      { date: '2026-05-15', count: 3, size: 300000000, duration: 900 },
      { date: '2026-06-02', count: 12, size: 1500000000, duration: 4200 },
      { date: '2026-06-20', count: 6, size: 640000000, duration: 1800 },
    ],
  }
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getMediaTrends(): Promise<MediaTrends> {
  return useMock ? mockGetMediaTrends() : realGetMediaTrends()
}
