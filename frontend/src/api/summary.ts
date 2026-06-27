import type { LibrarySummary } from '@/types'
import client from './client'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── 真实 API 实现 ──────────────────────────────────

async function realGetLibrarySummary(): Promise<LibrarySummary> {
  const res = await client.get<LibrarySummary>('/api/library/summary')
  return res.data
}

// ─── Mock API 实现 ──────────────────────────────────
// mock 模式给一组示例聚合，便于离线 demo 展示概览各分区。

async function mockGetLibrarySummary(): Promise<LibrarySummary> {
  await mockDelay(120)
  return {
    total: 12480,
    video_count: 3210,
    image_count: 9270,
    total_size: 1979900000000,
    total_duration: 2311200,
    library_count: 3,
    by_library: [
      { library_id: 1, label: '电影', media_count: 3460, video_count: 3460, image_count: 0, total_size: 580000000000, total_duration: 1980000 },
      { library_id: 2, label: '照片', media_count: 8200, video_count: 120, image_count: 8080, total_size: 320000000000, total_duration: 28800 },
      { library_id: 3, label: '剧集', media_count: 820, video_count: 820, image_count: 0, total_size: 1079900000000, total_duration: 302400 },
    ],
  }
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getLibrarySummary(): Promise<LibrarySummary> {
  return useMock ? mockGetLibrarySummary() : realGetLibrarySummary()
}
