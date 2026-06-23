import { http, HttpResponse, delay } from 'msw'
import { mockPaths, mockMediaFiles } from './data'
import type { LibraryPath, MediaFile, MediaExtension, Album, Tag, ScanTask } from '@/types'

// 内存中的可变数据（支持增删）
let paths = [...mockPaths]
let mediaFiles = [...mockMediaFiles]
let mediaExtensions: MediaExtension[] = []
let nextPathId = Math.max(...paths.map(p => p.id)) + 1
let nextMediaId = Math.max(...mediaFiles.map(m => m.id)) + 1
let nextExtensionId = 1
// 运行期设置内存存储（支持读写往返）
const settingsStore: Record<string, string> = {
  scan_interval: '3600',
  recycle_bin_paths: '{"D":"D:/.recycle"}',
  ffmpeg_path: '',
  ffprobe_path: '',
}

// 相册（FR-40）内存数据
let albums: Album[] = []
let albumItems: { album_id: number; media_id: number }[] = []
let nextAlbumId = 1

// 标签（FR-41）
const tags: Tag[] = []
let tagMappings: { tag_id: number; media_id: number }[] = []
let nextTagId = 1

// 软删除/回收站（FR-25）：被软删的媒体 ID 集合
const deletedMediaIds = new Set<number>()

// 扫描任务队列（FR-29）内存数据
const scanTasks: ScanTask[] = []
let nextScanTaskId = 1

export const handlers = [
  // ─── 认证 ───────────────────────────────────────────

  http.post('*/api/auth/login', async ({ request }) => {
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

  http.post('*/api/auth/logout', async () => {
    await delay(100)
    return new HttpResponse(null, {
      headers: { 'Set-Cookie': 'auth_token=; Path=/; Max-Age=0' },
    })
  }),

  http.get('*/api/me', async () => {
    await delay(100)
    return HttpResponse.json({ username: 'admin' })
  }),

  // ─── 媒体库目录 ─────────────────────────────────────

  http.get('*/api/library/paths', async () => {
    await delay(200)
    // 为每个库附带已索引媒体数量，与真实接口字段一致
    const items = paths.map(p => ({
      ...p,
      media_count: mediaFiles.filter(m => m.library_id === p.id).length,
    }))
    return HttpResponse.json({ items })
  }),

  http.post('*/api/library/paths', async ({ request }) => {
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

  http.delete('*/api/library/paths/:id', async ({ params }) => {
    await delay(200)
    const id = Number(params.id)
    paths = paths.filter(p => p.id !== id)
    // 同时删除关联的媒体文件与自定义后缀
    mediaFiles = mediaFiles.filter(m => m.library_id !== id)
    mediaExtensions = mediaExtensions.filter(ext => ext.library_id !== id)
    return new HttpResponse(null, { status: 204 })
  }),

  // ─── 媒体文件 ───────────────────────────────────────

  http.get('*/api/library/media', async ({ request }) => {
    await delay(300)
    const url = new URL(request.url)
    const page = Number(url.searchParams.get('page') || '1')
    const pageSize = Number(url.searchParams.get('page_size') || '20')
    const search = url.searchParams.get('search') || ''
    const sort = url.searchParams.get('sort') || ''
    const favorite = url.searchParams.get('favorite')
    const tagID = Number(url.searchParams.get('tag_id') || '0')

    // 常规列表排除已软删项（FR-25）
    let items = mediaFiles.filter(m => !deletedMediaIds.has(m.id))
    if (search) {
      items = items.filter(m => m.file_name.toLowerCase().includes(search.toLowerCase()))
    }
    if (favorite === 'true' || favorite === '1') {
      items = items.filter(m => m.favorite)
    }
    if (tagID > 0) {
      const ids = new Set(tagMappings.filter(tm => tm.tag_id === tagID).map(tm => tm.media_id))
      items = items.filter(m => ids.has(m.id))
    }
    if (sort === 'time_desc') {
      items.sort((a, b) => b.added_at.localeCompare(a.added_at))
    }

    const total = items.length
    const start = (page - 1) * pageSize
    const paged = items.slice(start, start + pageSize)

    return HttpResponse.json({ items: paged, total, page, page_size: pageSize })
  }),

  // ─── 收藏与标签（FR-41）──────────────────────────────

  // 真实文件名改名（FR-30）：仅模拟更新 file_name，不动 display_name
  http.put('*/api/library/media/:id/rename', async ({ request, params }) => {
    await delay(100)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    const body = await request.json() as { new_name: string }
    const newName = (body.new_name || '').trim()
    if (!newName || /[/\\]/.test(newName) || newName === '.' || newName === '..') {
      return HttpResponse.json({ code: 'RENAME_REJECTED', message: '新文件名不合法' }, { status: 400 })
    }
    file.file_name = newName
    return HttpResponse.json(file)
  }),

  // 显示名修改（FR-30）：仅更新库内 display_name，不动磁盘真实文件名
  http.put('*/api/library/media/:id/display-name', async ({ request, params }) => {
    await delay(100)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    const body = await request.json() as { display_name: string }
    file.display_name = (body.display_name || '').trim()
    return HttpResponse.json(file)
  }),

  http.put('*/api/library/media/:id/favorite', async ({ request, params }) => {
    await delay(100)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    const body = await request.json() as { favorite: boolean }
    file.favorite = body.favorite
    return HttpResponse.json(file)
  }),

  http.get('*/api/library/tags', async () => {
    await delay(100)
    return HttpResponse.json({ items: [...tags].sort((a, b) => a.name.localeCompare(b.name)) })
  }),

  http.post('*/api/library/tags', async ({ request }) => {
    await delay(100)
    const body = await request.json() as { name: string }
    const name = (body.name || '').trim()
    if (!name) {
      return HttpResponse.json({ code: 'INVALID_TAG', message: '标签名不能为空' }, { status: 400 })
    }
    let tag = tags.find(t => t.name === name)
    if (!tag) {
      tag = { id: nextTagId++, name, created_at: new Date().toISOString() }
      tags.push(tag)
    }
    return HttpResponse.json(tag, { status: 201 })
  }),

  http.get('*/api/library/media/:id/tags', async ({ params }) => {
    await delay(100)
    const id = Number(params.id)
    const ids = new Set(tagMappings.filter(tm => tm.media_id === id).map(tm => tm.tag_id))
    return HttpResponse.json({ items: tags.filter(t => ids.has(t.id)).sort((a, b) => a.name.localeCompare(b.name)) })
  }),

  http.post('*/api/library/media/:id/tags', async ({ request, params }) => {
    await delay(100)
    const mediaID = Number(params.id)
    const body = await request.json() as { tag_id?: number; name?: string }
    let tag: Tag | undefined
    if (body.tag_id) {
      tag = tags.find(t => t.id === body.tag_id)
    } else if (body.name) {
      const name = body.name.trim()
      if (!name) return HttpResponse.json({ code: 'INVALID_TAG', message: '标签名不能为空' }, { status: 400 })
      tag = tags.find(t => t.name === name)
      if (!tag) {
        tag = { id: nextTagId++, name, created_at: new Date().toISOString() }
        tags.push(tag)
      }
    }
    if (!tag) return HttpResponse.json({ code: 'ADD_TAG_FAILED', message: '标签不存在' }, { status: 400 })
    if (!tagMappings.some(tm => tm.tag_id === tag!.id && tm.media_id === mediaID)) {
      tagMappings.push({ tag_id: tag.id, media_id: mediaID })
    }
    return HttpResponse.json(tag, { status: 201 })
  }),

  http.delete('*/api/library/media/:id/tags/:tagId', async ({ params }) => {
    await delay(100)
    const mediaID = Number(params.id)
    const tagID = Number(params.tagId)
    tagMappings = tagMappings.filter(tm => !(tm.tag_id === tagID && tm.media_id === mediaID))
    return new HttpResponse(null, { status: 204 })
  }),

  // ─── 续播与观看状态（FR-44）─────────────────────────

  http.get('*/api/library/continue-watching', async ({ request }) => {
    await delay(100)
    const limit = Number(new URL(request.url).searchParams.get('limit') || '12')
    const items = mediaFiles
      .filter(m => (m.last_position ?? 0) > 0 && !m.watched)
      .sort((a, b) => (b.last_watched_at ?? '').localeCompare(a.last_watched_at ?? ''))
      .slice(0, limit)
    return HttpResponse.json({ items })
  }),

  // 编码协商（FR-53）：默认返回 h264/TS 描述符，使既有播放流程沿用 master 探测；
  // 需要 fMP4 路径的用例在测试中用 server.use 覆盖。
  http.post('*/api/play/:id/negotiate', async ({ params }) => {
    await delay(50)
    const id = Number(params.id)
    return HttpResponse.json({ codec: 'h264', path: 'ts', url: `/api/play/hls/${id}/master` })
  }),

  http.put('*/api/play/:id/position', async ({ request, params }) => {
    await delay(100)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    const body = await request.json() as { position: number }
    file.last_position = body.position < 0 ? 0 : body.position
    file.last_watched_at = new Date().toISOString()
    return HttpResponse.json(file)
  }),

  http.put('*/api/play/:id/watched', async ({ params }) => {
    await delay(100)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    file.watched = true
    file.last_position = 0
    file.last_watched_at = new Date().toISOString()
    return HttpResponse.json(file)
  }),

  http.get('*/api/library/media/:id/raw', async ({ params }) => {
    await delay(100)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    return new HttpResponse('', { headers: { 'Content-Type': `image/${file.format}` } })
  }),

  // 下载原文件（FR-42）：以附件形式回传原始文件
  http.get('*/api/library/media/:id/download', async ({ params }) => {
    await delay(100)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    return new HttpResponse('mock-file-bytes', {
      headers: { 'Content-Disposition': `attachment; filename*=UTF-8''${encodeURIComponent(file.file_name)}` },
    })
  }),

  http.get('*/api/library/media/:id', async ({ params }) => {
    await delay(200)
    const id = Number(params.id)
    const file = mediaFiles.find(m => m.id === id)
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 })
    }
    return HttpResponse.json(file)
  }),

  http.get('*/api/library/extensions', async ({ request }) => {
    await delay(100)
    const libraryID = Number(new URL(request.url).searchParams.get('library_id') || '0')
    return HttpResponse.json({ items: mediaExtensions.filter(ext => ext.library_id === libraryID) })
  }),

  http.post('*/api/library/extensions', async ({ request }) => {
    await delay(100)
    const body = await request.json() as { library_id: number; extension: string; type: 'image' | 'video' }
    const extension = body.extension.trim().toLowerCase().replace(/^\./, '')
    if (!extension) {
      return HttpResponse.json({ code: 'INVALID_EXTENSION', message: '后缀格式不支持' }, { status: 400 })
    }
    if (!mediaExtensions.some(ext => ext.library_id === body.library_id && ext.extension === extension)) {
      mediaExtensions.push({
        id: nextExtensionId++,
        library_id: body.library_id,
        extension,
        type: body.type,
        is_builtin: 0,
        created_at: new Date().toISOString(),
      })
    }
    return new HttpResponse(null, { status: 201 })
  }),

  // 删除自定义后缀（FR-64）：内置不可删、删不存在返回 400
  http.delete('*/api/library/extensions', async ({ request }) => {
    await delay(100)
    const url = new URL(request.url)
    const libraryID = Number(url.searchParams.get('library_id') || '0')
    const extension = (url.searchParams.get('extension') || '').trim().toLowerCase().replace(/^\./, '')
    const idx = mediaExtensions.findIndex(ext => ext.library_id === libraryID && ext.extension === extension && ext.is_builtin === 0)
    if (idx === -1) {
      return HttpResponse.json({ code: 'DELETE_EXTENSION_FAILED', message: '自定义后缀不存在' }, { status: 400 })
    }
    mediaExtensions.splice(idx, 1)
    return new HttpResponse(null, { status: 204 })
  }),

  http.delete('*/api/library/media/:id', async ({ params }) => {
    await delay(200)
    const id = Number(params.id)
    // 软删除（FR-25）：仅标记，不从内存数据移除
    if (mediaFiles.some(m => m.id === id)) deletedMediaIds.add(id)
    return new HttpResponse(null, { status: 204 })
  }),

  // ─── 软删除与回收站（FR-25）──────────────────────────

  http.get('*/api/library/recycle', async () => {
    await delay(200)
    const items = mediaFiles.filter(m => deletedMediaIds.has(m.id))
    return HttpResponse.json({ items })
  }),

  http.post('*/api/library/media/:id/restore', async ({ params }) => {
    await delay(200)
    const id = Number(params.id)
    if (!deletedMediaIds.has(id)) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '回收站中不存在该媒体文件' }, { status: 404 })
    }
    deletedMediaIds.delete(id)
    return new HttpResponse(null, { status: 204 })
  }),

  // 回收站清理（FR-26）：把全部软删项移出（模拟移到回收站目录 + 删记录）
  http.post('*/api/library/recycle/cleanup', async () => {
    await delay(200)
    let moved = 0
    for (const id of deletedMediaIds) {
      mediaFiles = mediaFiles.filter(m => m.id !== id)
      moved++
    }
    deletedMediaIds.clear()
    return HttpResponse.json({ moved, failed: 0 })
  }),

  // ─── 目录浏览 ─────────────────────────────────────────

  http.get('*/api/library/browse', async ({ request }) => {
    await delay(200)
    const url = new URL(request.url)
    const libraryID = Number(url.searchParams.get('library_id') || '0')
    const parentPath = url.searchParams.get('parent_path') || '/'

    // 聚合虚拟根（FR-66）：列出所有启用库作为顶层目录、各项携带 library_id
    if (parentPath === '__root__') {
      return HttpResponse.json({
        breadcrumbs: [{ name: '全部存储库', path: '__root__' }],
        directories: paths
          .filter(p => p.enabled)
          .map(p => ({ name: p.label || p.path, path: p.path, library_id: p.id })),
        files: [],
      })
    }

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
      current = /^[A-Za-z]:$/.test(p) && current === '' ? p : `${current.replace(/\/$/, '')}/${p}`
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

  http.get('*/api/play/:id/subtitles', async () => {
    await delay(150)
    return HttpResponse.json({
      tracks: [
        { index: 0, file_name: '电影名.srt', format: 'srt', url: '/api/play/1/subtitles/0' },
        { index: 1, file_name: '电影名.ass', format: 'ass', url: '/api/play/1/subtitles/1' },
      ],
    })
  }),

  http.get('*/api/play/:id/subtitles/:index', async () => {
    await delay(100)
    return new HttpResponse(
      'WEBVTT\n\n1\n00:00:01.000 --> 00:00:03.000\n这是第一条测试字幕\n\n2\n00:00:04.000 --> 00:00:06.000\n这是第二条测试字幕\n',
      { headers: { 'Content-Type': 'text/vtt' } },
    )
  }),

  // ─── SMB 凭据 ──────────────────────────────────────────

  http.post('*/api/smb/credentials', async () => {
    await delay(200)
    return new HttpResponse(null, { status: 204 })
  }),

  // ─── 系统诊断 ──────────────────────────────────────────

  http.get('*/api/system/info', async () => {
    await delay(150)
    return HttpResponse.json({
      app_version: '0.3.0',
      os: 'linux',
      arch: 'amd64',
      num_cpu: 8,
      hostname: 'nas01',
      go_version: 'go1.22.5',
      ffmpeg: {
        available: true,
        path: '/opt/jianvideo/ffmpeg',
        version: 'ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers',
      },
      runtime: {
        pid: 12345,
        work_dir: '/opt/jianvideo',
        executable: '/opt/jianvideo/jianvideo',
        db_path: '/opt/jianvideo/data/jianvideo.db',
        uptime_seconds: 3661,
        mem_alloc: 12 * 1024 * 1024,
        mem_sys: 48 * 1024 * 1024,
        num_gc: 7,
        gomaxprocs: 8,
      },
      hwaccel: {
        available: [
          {
            name: 'AMD AMF',
            family: 'amf',
            device_type: 'd3d11va',
            available: true,
            codecs: [
              { codec: 'h264', encoder: 'h264_amf', compiled: true, tested_ok: true },
              { codec: 'h265', encoder: 'hevc_amf', compiled: true, tested_ok: true },
              { codec: 'av1', encoder: 'av1_amf', compiled: true, tested_ok: false },
            ],
          },
          {
            name: '软件编码',
            family: 'software',
            device_type: '',
            available: true,
            codecs: [
              { codec: 'h264', encoder: 'libx264', compiled: true, tested_ok: true },
              { codec: 'h265', encoder: 'libx265', compiled: true, tested_ok: true },
            ],
          },
        ],
        preferred: 'h264_amf',
        codecs: ['h264', 'h265'],
        intel_gpu: false,
        intel_gpu_detail: '',
        software_fallback: true,
        from_cache: true,
        ffmpeg_version: 'ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers',
        tested_at: '2026-06-23T10:00:00Z',
      },
    })
  }),

  http.post('*/api/system/codec-test', async () => {
    await delay(200)
    return HttpResponse.json({
      ffmpeg_available: true,
      results: [
        { encoder: 'libx264', family: 'software', codec: 'h264', compiled: true, tested_ok: true, detail: '' },
        { encoder: 'libx265', family: 'software', codec: 'h265', compiled: true, tested_ok: true, detail: '' },
        { encoder: 'h264_amf', family: 'amf', codec: 'h264', compiled: true, tested_ok: true, detail: '' },
        { encoder: 'hevc_amf', family: 'amf', codec: 'h265', compiled: true, tested_ok: true, detail: '' },
        {
          encoder: 'av1_amf',
          family: 'amf',
          codec: 'av1',
          compiled: true,
          tested_ok: false,
          detail: '[av1_amf @ 0x55] AMF 不支持 AV1 编码',
        },
      ],
      from_cache: true,
      ffmpeg_version: 'ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers',
      tested_at: '2026-06-23T10:00:00Z',
    })
  }),

  // 环境变量查看（FR-56）：只读，敏感项已脱敏（绝不含明文）
  http.get('*/api/system/env', async () => {
    await delay(80)
    return HttpResponse.json({
      env: [
        { key: 'JIANVIDEO_FFMPEG_PATH', description: 'ffmpeg 可执行文件路径，未设置时回退同目录捆绑版或 PATH', sensitive: false, set: true, value: '/opt/jianvideo/ffmpeg' },
        { key: 'JIANVIDEO_DEBUG', description: '设为 1/true 时启用 gin debug 模式（输出调试日志）', sensitive: false, set: false, value: '' },
        { key: 'JWT_SECRET', description: 'JWT 签名密钥，未设置时启动随机生成（重启后需重新登录）', sensitive: true, set: true, value: '****（已设置）' },
        { key: 'SMB_MASTER_PASSWORD', description: 'SMB 凭据加解密主密码，未设置则 SMB 凭据功能不可用', sensitive: true, set: false, value: '（未设置）' },
      ],
    })
  }),

  // FFmpeg 路径检测（FR-56）：含 ffmpeg 字样或空路径视为可用
  http.post('*/api/system/ffmpeg/detect', async ({ request }) => {
    await delay(120)
    const body = await request.json() as { path?: string }
    const path = body.path || ''
    const available = !path || path.toLowerCase().includes('ffmpeg')
    return HttpResponse.json({
      ffmpeg_available: available,
      ffmpeg_version: available ? 'ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers' : '',
    })
  }),

  // 自更新（FR-46）
  http.get('*/api/system/update/check', async ({ request }) => {
    await delay(150)
    const channel = new URL(request.url).searchParams.get('channel') || 'stable'
    const prerelease = channel === 'prerelease'
    return HttpResponse.json({
      current: '0.3.0',
      latest: prerelease ? 'v0.6.3-dev.abc1234' : 'v0.6.3',
      has_update: true,
      tag: prerelease ? 'v0.6.3-dev.abc1234' : 'v0.6.3',
      prerelease,
      channel,
      notes: '示例发布说明',
      asset_name: 'jianvideo-linux-amd64',
    })
  }),
  http.post('*/api/system/update/apply', async () => {
    await delay(100)
    return HttpResponse.json({ status: 'updating', message: '更新已应用，服务即将重启' })
  }),
  http.post('*/api/system/update/rollback', async () => {
    await delay(100)
    return HttpResponse.json({ status: 'rolling_back', message: '已回滚到上一版本，服务即将重启' })
  }),

  // ─── 运行期设置 ────────────────────────────────────────

  http.get('*/api/settings', async () => {
    await delay(100)
    return HttpResponse.json({ settings: { ...settingsStore } })
  }),

  http.put('*/api/settings', async ({ request }) => {
    await delay(100)
    const body = await request.json() as { settings: Record<string, string> }
    if (!body.settings || Object.keys(body.settings).length === 0) {
      return HttpResponse.json({ code: 'EMPTY_SETTINGS', message: 'settings 不能为空' }, { status: 400 })
    }
    Object.assign(settingsStore, body.settings)
    return HttpResponse.json({ settings: { ...settingsStore } })
  }),

  // ─── 扫描 ──────────────────────────────────────────────

  http.post('*/api/library/scan/:id', async ({ params }) => {
    await delay(500)
    const id = Number(params.id)
    const libraryPath = paths.find(p => p.id === id)?.path || 'D:\\Videos'
    // 模拟扫描：随机添加 1-3 个新文件
    const count = Math.floor(Math.random() * 3) + 1
    const formats = ['mp4', 'mkv', 'avi', 'mov']
    for (let i = 0; i < count; i++) {
      const fileId = nextMediaId++
      const format = formats[i % formats.length]
      const newFile: MediaFile = {
        id: fileId,
        library_id: id,
        file_path: `${libraryPath}\\scan_result-${fileId}.${format}`,
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
      }
      mediaFiles.push(newFile)
    }
    // 扫描任务队列（FR-29）：入队一条已完成任务，供页眉任务展示
    const now = new Date().toISOString()
    scanTasks.unshift({
      id: nextScanTaskId++, library_id: id, scan_type: 'full', status: 'completed',
      scanned_files: count, total_files: count, error: '',
      created_at: now, started_at: now, completed_at: now,
    })
    return HttpResponse.json({ status: 'queued', task_id: nextScanTaskId - 1 })
  }),

  // 扫描任务列表（FR-29）
  http.get('*/api/library/scan/tasks', async () => {
    await delay(50)
    const current = scanTasks.find(t => t.status === 'running') ?? null
    return HttpResponse.json({ tasks: [...scanTasks], current })
  }),

  // ─── 相册（FR-40）──────────────────────────────────────

  http.get('*/api/albums', async () => {
    await delay(120)
    const items = albums.map(a => ({
      ...a,
      item_count: albumItems.filter(it => it.album_id === a.id).length,
    }))
    return HttpResponse.json({ items })
  }),

  http.post('*/api/albums', async ({ request }) => {
    await delay(150)
    const body = await request.json() as { name: string; description?: string }
    const name = (body.name || '').trim()
    if (!name) {
      return HttpResponse.json({ code: 'INVALID_INPUT', message: '相册名称不能为空' }, { status: 400 })
    }
    const now = new Date().toISOString()
    const album: Album = {
      id: nextAlbumId++,
      name,
      description: (body.description || '').trim(),
      cover_media_id: 0,
      created_at: now,
      updated_at: now,
    }
    albums.unshift(album)
    return HttpResponse.json(album, { status: 201 })
  }),

  http.delete('*/api/albums/:id', async ({ params }) => {
    await delay(120)
    const id = Number(params.id)
    if (!albums.some(a => a.id === id)) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '相册不存在' }, { status: 404 })
    }
    albums = albums.filter(a => a.id !== id)
    albumItems = albumItems.filter(it => it.album_id !== id)
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('*/api/albums/:id/items', async ({ params }) => {
    await delay(120)
    const id = Number(params.id)
    const items = albumItems
      .filter(it => it.album_id === id)
      .map(it => mediaFiles.find(m => m.id === it.media_id))
      .filter((m): m is MediaFile => m !== undefined)
    return HttpResponse.json({ items })
  }),

  http.post('*/api/albums/:id/items', async ({ params, request }) => {
    await delay(120)
    const id = Number(params.id)
    const body = await request.json() as { media_id: number }
    if (!albums.some(a => a.id === id) || !mediaFiles.some(m => m.id === body.media_id)) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '相册或媒体不存在' }, { status: 404 })
    }
    if (!albumItems.some(it => it.album_id === id && it.media_id === body.media_id)) {
      albumItems.push({ album_id: id, media_id: body.media_id })
    }
    return new HttpResponse(null, { status: 204 })
  }),

  http.delete('*/api/albums/:id/items/:mediaId', async ({ params }) => {
    await delay(120)
    const id = Number(params.id)
    const mediaId = Number(params.mediaId)
    albumItems = albumItems.filter(it => !(it.album_id === id && it.media_id === mediaId))
    return new HttpResponse(null, { status: 204 })
  }),
]
