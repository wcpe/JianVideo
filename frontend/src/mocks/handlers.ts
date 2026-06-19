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

  // ─── 目录浏览 ─────────────────────────────────────────

  http.get('/api/library/browse', async ({ request }) => {
    await delay(200)
    const url = new URL(request.url)
    const libraryID = Number(url.searchParams.get('library_id') || '0')
    const parentPath = url.searchParams.get('parent_path') || '/'

    const prefix = parentPath.replace(/\\/g, '/') + '/'
    const allFiles = mediaFiles.filter(m => {
      const fp = m.file_path.replace(/\\/g, '/')
      return fp.startsWith(prefix) && m.library_id === libraryID
    })

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

    const cleanPath = parentPath.replace(/\\/g, '/').replace(/^\/+|\/+$/g, '')
    const parts = cleanPath.split('/').filter(Boolean)
    const breadcrumbs: { name: string; path: string }[] = []
    let current = ''
    for (const p of parts) {
      current += '/' + p
      breadcrumbs.push({ name: p, path: current })
    }
    if (breadcrumbs.length === 0) {
      breadcrumbs.push({ name: '/', path: '/' })
    }

    return HttpResponse.json({
      breadcrumbs,
      directories: Array.from(dirSet).sort().map(name => ({ name, path: prefix + name })),
      files,
    })
  }),

  // ─── 字幕 ─────────────────────────────────────────────

  http.get('/api/play/:id/subtitles', async () => {
    await delay(150)
    return HttpResponse.json({
      tracks: [
        { index: 0, file_name: '电影名.srt', format: 'srt', url: '/api/play/1/subtitles/0' },
        { index: 1, file_name: '电影名.ass', format: 'ass', url: '/api/play/1/subtitles/1' },
      ],
    })
  }),

  http.get('/api/play/:id/subtitles/:index', async () => {
    await delay(100)
    return new HttpResponse(
      'WEBVTT\n\n1\n00:00:01.000 --> 00:00:03.000\n这是第一条测试字幕\n\n2\n00:00:04.000 --> 00:00:06.000\n这是第二条测试字幕\n',
      { headers: { 'Content-Type': 'text/vtt' } },
    )
  }),

  // ─── SMB 凭据 ──────────────────────────────────────────

  http.post('/api/smb/credentials', async () => {
    await delay(200)
    return new HttpResponse(null, { status: 204 })
  }),

  // ─── 扫描 ──────────────────────────────────────────────

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
