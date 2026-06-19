/**
 * Mock 实现 — 所有 API 返回模拟数据，不发送网络请求
 * 用于无后端时的开发/演示
 *
 * 注意：可变数据（paths、mediaFiles）统一从 @/mocks/data 导入，
 *       避免与 library.ts 中的 mock 数据重复定义。
 */
import type { LibraryPath, MediaListResponse, MediaFile, ScanResponse } from '@/types'
import { mockPaths, mockMediaFiles } from '@/mocks/data'

// ─── 可变数据（引用共享源，通过 splice/push 修改） ────

let paths: LibraryPath[] = mockPaths
let mediaFiles: MediaFile[] = mockMediaFiles
let nextId = 100

function delay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── Mock API 实现 ──────────────────────────────────

export async function getLibraryPaths(): Promise<LibraryPath[]> {
  await delay(150)
  return [...paths]
}

export async function createLibraryPath(path: string, type = 'local', label = ''): Promise<LibraryPath> {
  await delay(200)
  const p: LibraryPath = {
    id: nextId++,
    path,
    type,
    label: label || path,
    enabled: true,
    created_at: new Date().toISOString(),
  }
  paths.push(p)
  return p
}

export async function deleteLibraryPath(id: number): Promise<void> {
  await delay(150)
  paths = paths.filter(p => p.id !== id)
  mediaFiles = mediaFiles.filter(m => m.library_id !== id)
}

export async function getMediaFiles(params: {
  library_id?: number
  sort?: string
  page?: number
  page_size?: number
  search?: string
} = {}): Promise<MediaListResponse> {
  await delay(200)
  const { page = 1, page_size = 20, search, sort } = params

  let items = [...mediaFiles]
  if (search) {
    items = items.filter(m => m.file_name.toLowerCase().includes(search.toLowerCase()))
  }
  if (sort === 'time_desc') {
    items.sort((a, b) => b.added_at.localeCompare(a.added_at))
  }

  const total = items.length
  const start = (page - 1) * page_size
  return { items: items.slice(start, start + page_size), total, page, page_size }
}

export async function getMediaFile(id: number): Promise<MediaFile> {
  await delay(100)
  const f = mediaFiles.find(m => m.id === id)
  if (!f) throw new Error('媒体文件不存在')
  return f
}

export async function deleteMediaFile(id: number): Promise<void> {
  await delay(150)
  mediaFiles = mediaFiles.filter(m => m.id !== id)
}

export async function scanLibrary(id: number): Promise<ScanResponse> {
  await delay(400)
  const libraryPath = paths.find(p => p.id === id)?.path || 'D:\\Videos'
  const count = Math.floor(Math.random() * 3) + 1
  const fmts = ['mp4', 'mkv', 'avi', 'mov']
  for (let i = 0; i < count; i++) {
    const fileId = nextId++
    const format = fmts[i % fmts.length]
    mediaFiles.push({
      id: fileId,
      library_id: id,
      file_path: `${libraryPath}\\scan-${fileId}.${format}`,
      file_name: `scan-result-${fileId}.${format}`,
      file_size: Math.floor(Math.random() * 5_000_000_000) + 500_000_000,
      format,
      video_codec: 'h264',
      audio_codec: 'aac',
      duration: Math.floor(Math.random() * 7200) + 600,
      width: 1920,
      height: 1080,
      bitrate: 5000000,
      subtitle_tracks: '',
      added_at: new Date().toISOString(),
      modified_at: new Date().toISOString(),
    })
  }
  return { scanned: count }
}
