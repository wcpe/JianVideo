import type { LibraryPath, MediaFile, MediaListResponse, ScanResponse } from '@/types'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

// ─── Mock 数据（统一来源） ────────────────────────────

import { mockPaths, mockMediaFiles } from '@/mocks/data'

let nextMockId = 100

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── 真实 API 实现 ──────────────────────────────────

import client from './client'

async function realGetLibraryPaths(): Promise<LibraryPath[]> {
  const res = await client.get<{ items: LibraryPath[] }>('/api/library/paths')
  return res.data.items
}

async function realCreateLibraryPath(path: string, type = 'local', label = ''): Promise<LibraryPath> {
  const res = await client.post<LibraryPath>('/api/library/paths', { path, type, label })
  return res.data
}

async function realDeleteLibraryPath(id: number): Promise<void> {
  await client.delete(`/api/library/paths/${id}`)
}

async function realGetMediaFiles(params: {
  library_id?: number; sort?: string; page?: number; page_size?: number; search?: string
} = {}): Promise<MediaListResponse> {
  const res = await client.get<MediaListResponse>('/api/library/media', { params })
  return res.data
}

async function realGetMediaFile(id: number): Promise<MediaFile> {
  const res = await client.get(`/api/library/media/${id}`)
  return res.data
}

async function realDeleteMediaFile(id: number): Promise<void> {
  await client.delete(`/api/library/media/${id}`)
}

async function realScanLibrary(id: number): Promise<ScanResponse> {
  const res = await client.post<ScanResponse>(`/api/library/scan/${id}`)
  return res.data
}

// ─── Mock API 实现 ──────────────────────────────────

async function mockGetLibraryPaths(): Promise<LibraryPath[]> {
  await mockDelay(150)
  return [...mockPaths]
}

async function mockCreateLibraryPath(path: string, type = 'local', label = ''): Promise<LibraryPath> {
  await mockDelay(200)
  const p: LibraryPath = { id: nextMockId++, path, type, label: label || path, enabled: true, created_at: new Date().toISOString() }
  mockPaths.push(p)
  return p
}

async function mockDeleteLibraryPath(id: number): Promise<void> {
  await mockDelay(150)
  const idx = mockPaths.findIndex(p => p.id === id)
  if (idx !== -1) mockPaths.splice(idx, 1)
  // 清理关联的 mockMediaFiles
  for (let i = mockMediaFiles.length - 1; i >= 0; i--) {
    if (mockMediaFiles[i].library_id === id) mockMediaFiles.splice(i, 1)
  }
}

async function mockGetMediaFiles(params: {
  library_id?: number; sort?: string; page?: number; page_size?: number; search?: string
} = {}): Promise<MediaListResponse> {
  await mockDelay(200)
  const { page = 1, page_size = 20, search } = params
  let items = [...mockMediaFiles]
  if (search) items = items.filter(m => m.file_name.toLowerCase().includes(search.toLowerCase()))
  const total = items.length
  const start = (page - 1) * page_size
  return { items: items.slice(start, start + page_size), total, page, page_size }
}

async function mockGetMediaFile(id: number): Promise<MediaFile> {
  await mockDelay(100)
  const f = mockMediaFiles.find(m => m.id === id)
  if (!f) throw new Error('媒体文件不存在')
  return f
}

async function mockDeleteMediaFile(id: number): Promise<void> {
  await mockDelay(150)
  const idx = mockMediaFiles.findIndex(m => m.id === id)
  if (idx !== -1) mockMediaFiles.splice(idx, 1)
}

async function mockScanLibrary(id: number): Promise<ScanResponse> {
  await mockDelay(400)
  const count = Math.floor(Math.random() * 3) + 1
  const fmts = ['mp4', 'mkv', 'avi', 'mov']
  for (let i = 0; i < count; i++) {
    mockMediaFiles.push({
      id: nextMockId++, library_id: id,
      file_path: `D:\\Videos\\scan-${nextMockId}.${fmts[i % fmts.length]}`,
      file_name: `scan-result-${nextMockId}.${fmts[i % fmts.length]}`,
      file_size: Math.floor(Math.random() * 5_000_000_000) + 500_000_000,
      format: fmts[i % fmts.length], video_codec: 'h264', audio_codec: 'aac',
      duration: Math.floor(Math.random() * 7200) + 600, width: 1920, height: 1080,
      bitrate: 5000000, subtitle_tracks: '',
      added_at: new Date().toISOString(), modified_at: new Date().toISOString(),
    })
  }
  return { scanned: count }
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getLibraryPaths() { return useMock ? mockGetLibraryPaths() : realGetLibraryPaths() }
export function createLibraryPath(p: string, t = 'local', l = '') { return useMock ? mockCreateLibraryPath(p, t, l) : realCreateLibraryPath(p, t, l) }
export function deleteLibraryPath(id: number) { return useMock ? mockDeleteLibraryPath(id) : realDeleteLibraryPath(id) }
export function getMediaFiles(params?: { library_id?: number; sort?: string; page?: number; page_size?: number; search?: string }) { return useMock ? mockGetMediaFiles(params) : realGetMediaFiles(params) }
export function getMediaFile(id: number) { return useMock ? mockGetMediaFile(id) : realGetMediaFile(id) }
export function deleteMediaFile(id: number) { return useMock ? mockDeleteMediaFile(id) : realDeleteMediaFile(id) }
export function scanLibrary(id: number) { return useMock ? mockScanLibrary(id) : realScanLibrary(id) }
