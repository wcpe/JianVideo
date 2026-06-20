import type { LibraryPath, MediaFile, MediaListResponse, ScanResponse, BrowseResponse, MediaExtension, MediaExtensionType, ScanStatus } from '@/types'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

// ─── Mock 数据（统一来源） ────────────────────────────

import { mockPaths, mockMediaFiles } from '@/mocks/data'

let nextMockId = 100
let nextMockExtensionId = 1
const mockExtensions: MediaExtension[] = []

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── 真实 API 实现 ──────────────────────────────────

import client from './client'
import type { AxiosError } from 'axios'

function getApiErrorMessage(err: unknown, fallback: string): string {
  const error = err as AxiosError<{ message?: string }>
  return error.response?.data?.message || (err instanceof Error ? err.message : fallback)
}

async function realGetLibraryPaths(): Promise<LibraryPath[]> {
  const res = await client.get<{ items: LibraryPath[] }>('/api/library/paths')
  return res.data.items
}

async function realCreateLibraryPath(path: string, type = 'local', label = ''): Promise<LibraryPath> {
  try {
    const res = await client.post<LibraryPath>('/api/library/paths', { path, type, label })
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '无法添加目录，请检查路径是否正确'))
  }
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

async function realBrowseDirectory(libraryID: number, parentPath: string): Promise<BrowseResponse> {
  try {
    const res = await client.get<BrowseResponse>('/api/library/browse', {
      params: { library_id: libraryID, parent_path: parentPath },
    })
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '加载目录内容失败，请重试'))
  }
}

async function realAddMediaExtension(libraryID: number, extension: string, type: MediaExtensionType): Promise<void> {
  try {
    await client.post('/api/library/extensions', { library_id: libraryID, extension, type })
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '添加后缀失败，请检查格式'))
  }
}

async function realListMediaExtensions(libraryID: number): Promise<MediaExtension[]> {
  const res = await client.get<{ items: MediaExtension[] }>('/api/library/extensions', {
    params: { library_id: libraryID },
  })
  return res.data.items
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
  for (let i = mockExtensions.length - 1; i >= 0; i--) {
    if (mockExtensions[i].library_id === id) mockExtensions.splice(i, 1)
  }
}

async function mockGetMediaFiles(params: {
  library_id?: number; sort?: string; page?: number; page_size?: number; search?: string
} = {}): Promise<MediaListResponse> {
  await mockDelay(200)
  const { page = 1, page_size = 20, search, sort } = params
  let items = [...mockMediaFiles]
  if (search) items = items.filter(m => m.file_name.toLowerCase().includes(search.toLowerCase()))
  if (sort === 'time_desc') items.sort((a, b) => b.added_at.localeCompare(a.added_at))
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
  const libraryPath = mockPaths.find(p => p.id === id)?.path || 'D:\\Videos'
  const count = Math.floor(Math.random() * 3) + 1
  const fmts = ['mp4', 'mkv', 'avi', 'mov']
  for (let i = 0; i < count; i++) {
    const fileId = nextMockId++
    const format = fmts[i % fmts.length]
    mockMediaFiles.push({
      id: fileId, library_id: id,
      file_path: `${libraryPath}\\scan-${fileId}.${format}`,
      file_name: `scan-result-${fileId}.${format}`,
      file_size: Math.floor(Math.random() * 5_000_000_000) + 500_000_000,
      format, video_codec: 'h264', audio_codec: 'aac',
      duration: Math.floor(Math.random() * 7200) + 600, width: 1920, height: 1080,
      bitrate: 5000000, subtitle_tracks: '',
      added_at: new Date().toISOString(), modified_at: new Date().toISOString(),
    })
  }
  return { scanned: count }
}

async function mockAddMediaExtension(libraryID: number, extension: string, type: MediaExtensionType): Promise<void> {
  await mockDelay(100)
  const normalized = extension.trim().toLowerCase().replace(/^\./, '')
  if (!normalized) throw new Error('后缀格式不支持')
  if (mockExtensions.some(ext => ext.library_id === libraryID && ext.extension === normalized)) return
  mockExtensions.push({
    id: nextMockExtensionId++,
    library_id: libraryID,
    extension: normalized,
    type,
    is_builtin: 0,
    created_at: new Date().toISOString(),
  })
}

async function mockListMediaExtensions(libraryID: number): Promise<MediaExtension[]> {
  await mockDelay(100)
  return mockExtensions.filter(ext => ext.library_id === libraryID)
}

async function mockBrowseDirectory(libraryID: number, parentPath: string): Promise<BrowseResponse> {
  await mockDelay(150)
  // 从 mockMediaFiles 中筛选匹配前缀的文件
  const prefix = parentPath.replace(/\\/g, '/') + '/'
  const allFiles = mockMediaFiles.filter(m => {
    const fp = m.file_path.replace(/\\/g, '/')
    return fp.startsWith(prefix) && m.library_id === libraryID
  })

  // 按第一级子目录分组
  const dirSet = new Set<string>()
  const files: typeof allFiles = []
  for (const f of allFiles) {
    const rel = f.file_path.replace(/\\/g, '/').replace(prefix, '')
    const slashIdx = rel.indexOf('/')
    if (slashIdx !== -1) {
      dirSet.add(rel.substring(0, slashIdx))
    } else {
      files.push(f)
    }
  }

  // 构建面包屑，Windows 盘符不加前导斜杠
  const cleanPath = parentPath.replace(/\\/g, '/').replace(/^\/+|\/+$/g, '')
  const parts = cleanPath.split('/').filter(Boolean)
  const breadcrumbs: { name: string; path: string }[] = []
  let current = ''
  for (const p of parts) {
    current = /^[A-Za-z]:$/.test(p) && current === '' ? p : `${current.replace(/\/$/, '')}/${p}`
    breadcrumbs.push({ name: p, path: current })
  }
  if (breadcrumbs.length === 0) {
    breadcrumbs.push({ name: '/', path: '/' })
  }

  return {
    breadcrumbs,
    directories: Array.from(dirSet).sort().map(name => ({ name, path: prefix + name })),
    files,
  }
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getLibraryPaths() { return useMock ? mockGetLibraryPaths() : realGetLibraryPaths() }
export function createLibraryPath(p: string, t = 'local', l = '') { return useMock ? mockCreateLibraryPath(p, t, l) : realCreateLibraryPath(p, t, l) }
export function deleteLibraryPath(id: number) { return useMock ? mockDeleteLibraryPath(id) : realDeleteLibraryPath(id) }
export function getMediaFiles(params?: { library_id?: number; sort?: string; page?: number; page_size?: number; search?: string }) { return useMock ? mockGetMediaFiles(params) : realGetMediaFiles(params) }
export function getMediaFile(id: number) { return useMock ? mockGetMediaFile(id) : realGetMediaFile(id) }
export function deleteMediaFile(id: number) { return useMock ? mockDeleteMediaFile(id) : realDeleteMediaFile(id) }
export function scanLibrary(id: number) { return useMock ? mockScanLibrary(id) : realScanLibrary(id) }

/**
 * 创建扫描进度 SSE 连接，返回关闭函数。
 * mock 模式或运行环境无 EventSource（如测试 jsdom）时立即返回空关闭函数。
 */
export function createScanProgressSSE(onProgress: (status: ScanStatus) => void): () => void {
  if (useMock || typeof EventSource === 'undefined') {
    return () => {}
  }
  const eventSource = new EventSource('/api/library/scan/progress')
  eventSource.addEventListener('progress', (e) => {
    onProgress(JSON.parse(e.data) as ScanStatus)
  })
  return () => eventSource.close()
}
export function browseDirectory(libraryID: number, parentPath: string) { return useMock ? mockBrowseDirectory(libraryID, parentPath) : realBrowseDirectory(libraryID, parentPath) }
export function addMediaExtension(libraryID: number, extension: string, type: MediaExtensionType) { return useMock ? mockAddMediaExtension(libraryID, extension, type) : realAddMediaExtension(libraryID, extension, type) }
export function listMediaExtensions(libraryID: number) { return useMock ? mockListMediaExtensions(libraryID) : realListMediaExtensions(libraryID) }
