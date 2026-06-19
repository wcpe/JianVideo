import { http, HttpResponse, delay } from 'msw'
import { mockPaths, mockMediaFiles } from './data'
import type { LibraryPath, MediaFile } from '@/types'

// 内存中的可变数据（支持增删）
let paths = [...mockPaths]
let mediaFiles = [...mockMediaFiles]
let nextPathId = Math.max(...paths.map(p => p.id)) + 1
let nextMediaId = Math.max(...mediaFiles.map(m => m.id)) + 1

export const handlers = [
  // ─── 认证 ───────────────────────────────────────────

  http.post('/api/auth/login', async ({ request }) => {
    await delay(300)
    const body = await request.json() as { username: string; password: string }
    if (body.username === 'admin' && body.password === 'admin') {
      return HttpResponse.json(
        { username: 'admin' },
        {
          headers: { 'Set-Cookie': 'auth_token=mock_jwt; Path=/; HttpOnly; Secure; Max-Age=259200' },
        },
      )
    }
    return HttpResponse.json(
      { code: 'INVALID_CREDENTIALS', message: '用户名或密码错误' },
      { status: 401 },
    )
  }),

  http.post('/api/auth/logout', async () => {
    await delay(100)
    return new HttpResponse(null, {
      headers: { 'Set-Cookie': 'auth_token=; Path=/; Max-Age=0' },
    })
  }),

  http.get('/api/me', async () => {
    await delay(100)
    return HttpResponse.json({ username: 'admin' })
  }),

  // ─── 媒体库目录 ─────────────────────────────────────

  http.get('/api/library/paths', async () => {
    await delay(200)
    return HttpResponse.json({ items: [...paths] })
  }),

  http.post('/api/library/paths', async ({ request }) => {
    await delay(300)
    const body = await request.json() as { path: string; type: string; label: string }
    const newPath: LibraryPath = {
      id: nextPathId++,
      path: body.path,
      type: body.type || 'local',
      label: body.label || body.path,
      enabled: true,
      created_at: new Date().toISOString(),
    }
    paths.push(newPath)
    return HttpResponse.json(newPath, { status: 201 })
  }),

  http.delete('/api/library/paths/:id', async ({ params }) => {
    await delay(200)
    const id = Number(params.id)
    paths = paths.filter(p => p.id !== id)
    // 同时删除关联的媒体文件
    mediaFiles = mediaFiles.filter(m => m.library_id !== id)
    return new HttpResponse(null, { status: 204 })
  }),

  // ─── 媒体文件 ───────────────────────────────────────

  http.get('/api/library/media', async ({ request }) => {
    await delay(300)
    const url = new URL(request.url)
    const page = Number(url.searchParams.get('page') || '1')
    const pageSize = Number(url.searchParams.get('page_size') || '20')
    const search = url.searchParams.get('search') || ''

    let items = [...mediaFiles]
    if (search) {
      items = items.filter(m => m.file_name.toLowerCase().includes(search.toLowerCase()))
    }

    const total = items.length
    const start = (page - 1) * pageSize
    const paged = items.slice(start, start + pageSize)

    return HttpResponse.json({ items: paged, total, page, page_size: pageSize })
  }),

  http.get('/api/library/media/:id', async ({ params }) => {
    await delay(200)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    return HttpResponse.json(file)
  }),

  http.delete('/api/library/media/:id', async ({ params }) => {
    await delay(200)
    const id = Number(params.id)
    mediaFiles = mediaFiles.filter(m => m.id !== id)
    return new HttpResponse(null, { status: 204 })
  }),

  http.post('/api/library/scan/:id', async ({ params }) => {
    await delay(500)
    const id = Number(params.id)
    // 模拟扫描：随机添加 1-3 个新文件
    const count = Math.floor(Math.random() * 3) + 1
    const formats = ['mp4', 'mkv', 'avi', 'mov']
    for (let i = 0; i < count; i++) {
      const newFile: MediaFile = {
        id: nextMediaId++,
        library_id: id,
        file_path: `D:\\Videos\\scan_result-${nextMediaId}.${formats[i % formats.length]}`,
        file_name: `scan-result-${nextMediaId}.${formats[i % formats.length]}`,
        file_size: Math.floor(Math.random() * 5_000_000_000) + 500_000_000,
        format: formats[i % formats.length],
        video_codec: 'h264',
        audio_codec: 'aac',
        duration: Math.floor(Math.random() * 7200) + 600,
        width: 1920,
        height: 1080,
        bitrate: 5000000,
        subtitle_tracks: '',
        added_at: new Date().toISOString(),
        modified_at: new Date().toISOString(),
      }
      mediaFiles.push(newFile)
    }
    return HttpResponse.json({ scanned: count })
  }),
]
