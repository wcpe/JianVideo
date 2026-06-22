import type { LibraryPath, MediaFile, MediaListResponse, ScanResponse, ScanMode, BrowseResponse, MediaExtension, MediaExtensionType, ScanStatus, Tag, RecycleCleanupResult } from '@/types'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

// ─── Mock 数据（统一来源） ────────────────────────────

import { mockPaths, mockMediaFiles } from '@/mocks/data'

let nextMockId = 100
let nextMockExtensionId = 1
const mockExtensions: MediaExtension[] = []

// 标签 mock 状态（FR-41）：标签表 + 媒体-标签映射
let nextMockTagId = 1
const mockTags: Tag[] = []
const mockTagMappings: { tag_id: number; media_id: number }[] = []

// 软删除/回收站 mock 状态（FR-25）：被软删的媒体 ID 集合
const mockDeletedIds = new Set<number>()

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
    throw new Error(getApiErrorMessage(err, '无法添加目录，请检查路径是否正确'), { cause: err })
  }
}

async function realDeleteLibraryPath(id: number): Promise<void> {
  await client.delete(`/api/library/paths/${id}`)
}

async function realGetMediaFiles(params: {
  library_id?: number; sort?: string; page?: number; page_size?: number; search?: string; favorite?: boolean; tag_id?: number
} = {}): Promise<MediaListResponse> {
  const res = await client.get<MediaListResponse>('/api/library/media', { params })
  return res.data
}

// ─── 收藏与标签（FR-41）──────────────────────────────

async function realSetMediaFavorite(id: number, favorite: boolean): Promise<MediaFile> {
  const res = await client.put<MediaFile>(`/api/library/media/${id}/favorite`, { favorite })
  return res.data
}

async function realGetTags(): Promise<Tag[]> {
  const res = await client.get<{ items: Tag[] }>('/api/library/tags')
  return res.data.items
}

async function realCreateTag(name: string): Promise<Tag> {
  try {
    const res = await client.post<Tag>('/api/library/tags', { name })
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '创建标签失败'), { cause: err })
  }
}

async function realGetMediaTags(mediaID: number): Promise<Tag[]> {
  const res = await client.get<{ items: Tag[] }>(`/api/library/media/${mediaID}/tags`)
  return res.data.items
}

async function realAddMediaTag(mediaID: number, tag: { tag_id?: number; name?: string }): Promise<Tag> {
  try {
    const res = await client.post<Tag>(`/api/library/media/${mediaID}/tags`, tag)
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '打标签失败'), { cause: err })
  }
}

async function realRemoveMediaTag(mediaID: number, tagID: number): Promise<void> {
  await client.delete(`/api/library/media/${mediaID}/tags/${tagID}`)
}

// ─── 续播与观看状态（FR-44）─────────────────────────

async function realUpdateWatchPosition(id: number, position: number): Promise<MediaFile> {
  const res = await client.put<MediaFile>(`/api/play/${id}/position`, { position })
  return res.data
}

async function realMarkWatched(id: number): Promise<MediaFile> {
  const res = await client.put<MediaFile>(`/api/play/${id}/watched`)
  return res.data
}

async function realGetContinueWatching(limit = 12): Promise<MediaFile[]> {
  const res = await client.get<{ items: MediaFile[] }>('/api/library/continue-watching', { params: { limit } })
  return res.data.items
}

async function realGetMediaFile(id: number): Promise<MediaFile> {
  const res = await client.get(`/api/library/media/${id}`)
  return res.data
}

async function realDeleteMediaFile(id: number): Promise<void> {
  await client.delete(`/api/library/media/${id}`)
}

async function realRenameMediaFile(id: number, newName: string): Promise<MediaFile> {
  const res = await client.put<MediaFile>(`/api/library/media/${id}/rename`, { new_name: newName })
  return res.data
}

async function realUpdateDisplayName(id: number, displayName: string): Promise<MediaFile> {
  try {
    const res = await client.put<MediaFile>(`/api/library/media/${id}/display-name`, { display_name: displayName })
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '修改显示名失败'), { cause: err })
  }
}

// ─── 软删除与回收站（FR-25）──────────────────────────

async function realGetRecycleMediaFiles(): Promise<MediaFile[]> {
  const res = await client.get<{ items: MediaFile[] }>('/api/library/recycle')
  return res.data.items
}

async function realRestoreMediaFile(id: number): Promise<void> {
  await client.post(`/api/library/media/${id}/restore`)
}

// 回收站清理（FR-26）：把全部软删项源文件移到盘符回收站目录并删记录，返回移动/失败统计。
async function realCleanupRecycle(): Promise<RecycleCleanupResult> {
  try {
    const res = await client.post<RecycleCleanupResult>('/api/library/recycle/cleanup')
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '清理回收站失败'), { cause: err })
  }
}

async function realScanLibrary(id: number, mode: ScanMode = 'incremental'): Promise<ScanResponse> {
  const res = await client.post<ScanResponse>(`/api/library/scan/${id}`, null, { params: { mode } })
  return res.data
}

async function realBrowseDirectory(libraryID: number, parentPath: string): Promise<BrowseResponse> {
  try {
    const res = await client.get<BrowseResponse>('/api/library/browse', {
      params: { library_id: libraryID, parent_path: parentPath },
    })
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '加载目录内容失败，请重试'), { cause: err })
  }
}

async function realAddMediaExtension(libraryID: number, extension: string, type: MediaExtensionType): Promise<void> {
  try {
    await client.post('/api/library/extensions', { library_id: libraryID, extension, type })
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '添加后缀失败，请检查格式'), { cause: err })
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
  // 附带各库已索引媒体数量，与真实接口字段一致
  return mockPaths.map(p => ({
    ...p,
    media_count: mockMediaFiles.filter(m => m.library_id === p.id).length,
  }))
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
  library_id?: number; sort?: string; page?: number; page_size?: number; search?: string; favorite?: boolean; tag_id?: number
} = {}): Promise<MediaListResponse> {
  await mockDelay(200)
  const { page = 1, page_size = 20, search, sort, favorite, tag_id } = params
  // 常规列表排除已软删项（FR-25）
  let items = mockMediaFiles.filter(m => !mockDeletedIds.has(m.id))
  if (search) items = items.filter(m => m.file_name.toLowerCase().includes(search.toLowerCase()))
  if (favorite) items = items.filter(m => m.favorite)
  if (tag_id) {
    const ids = new Set(mockTagMappings.filter(tm => tm.tag_id === tag_id).map(tm => tm.media_id))
    items = items.filter(m => ids.has(m.id))
  }
  if (sort === 'time_desc') items.sort((a, b) => b.added_at.localeCompare(a.added_at))
  const total = items.length
  const start = (page - 1) * page_size
  return { items: items.slice(start, start + page_size), total, page, page_size }
}

// ─── 收藏与标签 mock（FR-41）─────────────────────────

async function mockSetMediaFavorite(id: number, favorite: boolean): Promise<MediaFile> {
  await mockDelay(100)
  const f = mockMediaFiles.find(m => m.id === id)
  if (!f) throw new Error('媒体文件不存在')
  f.favorite = favorite
  return f
}

async function mockGetTags(): Promise<Tag[]> {
  await mockDelay(100)
  return [...mockTags].sort((a, b) => a.name.localeCompare(b.name))
}

async function mockCreateTag(name: string): Promise<Tag> {
  await mockDelay(100)
  const normalized = name.trim()
  if (!normalized) throw new Error('标签名不能为空')
  const existing = mockTags.find(t => t.name === normalized)
  if (existing) return existing
  const tag: Tag = { id: nextMockTagId++, name: normalized, created_at: new Date().toISOString() }
  mockTags.push(tag)
  return tag
}

async function mockGetMediaTags(mediaID: number): Promise<Tag[]> {
  await mockDelay(100)
  const ids = new Set(mockTagMappings.filter(tm => tm.media_id === mediaID).map(tm => tm.tag_id))
  return mockTags.filter(t => ids.has(t.id)).sort((a, b) => a.name.localeCompare(b.name))
}

async function mockAddMediaTag(mediaID: number, tag: { tag_id?: number; name?: string }): Promise<Tag> {
  await mockDelay(100)
  let resolved: Tag | undefined
  if (tag.tag_id) resolved = mockTags.find(t => t.id === tag.tag_id)
  else if (tag.name) resolved = await mockCreateTag(tag.name)
  if (!resolved) throw new Error('标签不存在')
  if (!mockTagMappings.some(tm => tm.tag_id === resolved!.id && tm.media_id === mediaID)) {
    mockTagMappings.push({ tag_id: resolved.id, media_id: mediaID })
  }
  return resolved
}

async function mockRemoveMediaTag(mediaID: number, tagID: number): Promise<void> {
  await mockDelay(100)
  const idx = mockTagMappings.findIndex(tm => tm.tag_id === tagID && tm.media_id === mediaID)
  if (idx !== -1) mockTagMappings.splice(idx, 1)
}

// ─── 续播与观看状态 mock（FR-44）────────────────────

async function mockUpdateWatchPosition(id: number, position: number): Promise<MediaFile> {
  await mockDelay(100)
  const f = mockMediaFiles.find(m => m.id === id)
  if (!f) throw new Error('媒体文件不存在')
  f.last_position = position < 0 ? 0 : position
  f.last_watched_at = new Date().toISOString()
  return f
}

async function mockMarkWatched(id: number): Promise<MediaFile> {
  await mockDelay(100)
  const f = mockMediaFiles.find(m => m.id === id)
  if (!f) throw new Error('媒体文件不存在')
  f.watched = true
  f.last_position = 0
  f.last_watched_at = new Date().toISOString()
  return f
}

async function mockGetContinueWatching(limit = 12): Promise<MediaFile[]> {
  await mockDelay(100)
  return mockMediaFiles
    .filter(m => (m.last_position ?? 0) > 0 && !m.watched)
    .sort((a, b) => (b.last_watched_at ?? '').localeCompare(a.last_watched_at ?? ''))
    .slice(0, limit)
}

async function mockGetMediaFile(id: number): Promise<MediaFile> {
  await mockDelay(100)
  const f = mockMediaFiles.find(m => m.id === id)
  if (!f) throw new Error('媒体文件不存在')
  return f
}

async function mockDeleteMediaFile(id: number): Promise<void> {
  await mockDelay(150)
  // 软删除（FR-25）：仅标记，不从内存数据移除（源文件不动）
  if (mockMediaFiles.some(m => m.id === id)) mockDeletedIds.add(id)
}

async function mockGetRecycleMediaFiles(): Promise<MediaFile[]> {
  await mockDelay(150)
  return mockMediaFiles.filter(m => mockDeletedIds.has(m.id))
}

async function mockRestoreMediaFile(id: number): Promise<void> {
  await mockDelay(150)
  if (!mockDeletedIds.has(id)) throw new Error('回收站中不存在该媒体文件')
  mockDeletedIds.delete(id)
}

// 回收站清理 mock（FR-26）：把全部软删项从内存数据移除（模拟移到回收站目录 + 删记录）。
async function mockCleanupRecycle(): Promise<RecycleCleanupResult> {
  await mockDelay(200)
  let moved = 0
  for (const id of mockDeletedIds) {
    const idx = mockMediaFiles.findIndex(m => m.id === id)
    if (idx !== -1) mockMediaFiles.splice(idx, 1)
    moved++
  }
  mockDeletedIds.clear()
  return { moved, failed: 0 }
}

async function mockRenameMediaFile(id: number, newName: string): Promise<MediaFile> {
  await mockDelay(150)
  const f = mockMediaFiles.find(m => m.id === id)
  if (!f) throw new Error('媒体文件不存在')
  f.file_name = newName
  return f
}

async function mockUpdateDisplayName(id: number, displayName: string): Promise<MediaFile> {
  await mockDelay(120)
  const f = mockMediaFiles.find(m => m.id === id)
  if (!f) throw new Error('媒体文件不存在')
  f.display_name = displayName.trim()
  return f
}

async function mockScanLibrary(id: number, _mode: ScanMode = 'incremental'): Promise<ScanResponse> {
  await mockDelay(400)
  // mock 模式仅模拟新增入库，不区分对账（全量对账行为由后端集成测试覆盖）
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
export function getMediaFiles(params?: { library_id?: number; sort?: string; page?: number; page_size?: number; search?: string; favorite?: boolean; tag_id?: number }) { return useMock ? mockGetMediaFiles(params) : realGetMediaFiles(params) }
export function getMediaFile(id: number) { return useMock ? mockGetMediaFile(id) : realGetMediaFile(id) }
export function deleteMediaFile(id: number) { return useMock ? mockDeleteMediaFile(id) : realDeleteMediaFile(id) }
export function renameMediaFile(id: number, newName: string) { return useMock ? mockRenameMediaFile(id, newName) : realRenameMediaFile(id, newName) }
export function updateDisplayName(id: number, displayName: string) { return useMock ? mockUpdateDisplayName(id, displayName) : realUpdateDisplayName(id, displayName) }
// 软删除与回收站（FR-25）
export function getRecycleMediaFiles() { return useMock ? mockGetRecycleMediaFiles() : realGetRecycleMediaFiles() }
export function restoreMediaFile(id: number) { return useMock ? mockRestoreMediaFile(id) : realRestoreMediaFile(id) }
// 回收站清理（FR-26）
export function cleanupRecycle() { return useMock ? mockCleanupRecycle() : realCleanupRecycle() }
export function scanLibrary(id: number, mode: ScanMode = 'incremental') { return useMock ? mockScanLibrary(id, mode) : realScanLibrary(id, mode) }

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

// 收藏与标签（FR-41）
export function setMediaFavorite(id: number, favorite: boolean) { return useMock ? mockSetMediaFavorite(id, favorite) : realSetMediaFavorite(id, favorite) }
export function getTags() { return useMock ? mockGetTags() : realGetTags() }
export function createTag(name: string) { return useMock ? mockCreateTag(name) : realCreateTag(name) }
export function getMediaTags(mediaID: number) { return useMock ? mockGetMediaTags(mediaID) : realGetMediaTags(mediaID) }
export function addMediaTag(mediaID: number, tag: { tag_id?: number; name?: string }) { return useMock ? mockAddMediaTag(mediaID, tag) : realAddMediaTag(mediaID, tag) }
export function removeMediaTag(mediaID: number, tagID: number) { return useMock ? mockRemoveMediaTag(mediaID, tagID) : realRemoveMediaTag(mediaID, tagID) }

// 续播与观看状态（FR-44）
export function updateWatchPosition(id: number, position: number) { return useMock ? mockUpdateWatchPosition(id, position) : realUpdateWatchPosition(id, position) }
export function markWatched(id: number) { return useMock ? mockMarkWatched(id) : realMarkWatched(id) }
export function getContinueWatching(limit = 12) { return useMock ? mockGetContinueWatching(limit) : realGetContinueWatching(limit) }
export function addMediaExtension(libraryID: number, extension: string, type: MediaExtensionType) { return useMock ? mockAddMediaExtension(libraryID, extension, type) : realAddMediaExtension(libraryID, extension, type) }
export function listMediaExtensions(libraryID: number) { return useMock ? mockListMediaExtensions(libraryID) : realListMediaExtensions(libraryID) }
