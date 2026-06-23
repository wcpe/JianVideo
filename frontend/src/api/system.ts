import type { SystemInfo, CodecTestResult, UpdateCheckResult } from '@/types'
import client from './client'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── 真实 API 实现 ──────────────────────────────────

async function realGetSystemInfo(): Promise<SystemInfo> {
  const res = await client.get<SystemInfo>('/api/system/info')
  return res.data
}

async function realRunCodecTest(force?: boolean): Promise<CodecTestResult> {
  const res = await client.post<CodecTestResult>(
    '/api/system/codec-test',
    undefined,
    force ? { params: { force: 'true' } } : undefined,
  )
  return res.data
}

async function realCheckUpdate(channel: string, force = false): Promise<UpdateCheckResult> {
  // 检查更新需直连 GitHub，国内常较慢；单请求超时放宽到 60s（覆盖全局 15s），避免前端先于后端超时。
  const res = await client.get<UpdateCheckResult>('/api/system/update/check', {
    params: { channel, ...(force ? { force: 'true' } : {}) },
    timeout: 60000,
  })
  return res.data
}

async function realApplyUpdate(channel: string): Promise<void> {
  // 更新会触发下载替换，给较长单请求超时以防慢网络下前端提前超时。
  await client.post('/api/system/update/apply', { channel }, { timeout: 60000 })
}

async function realRollbackUpdate(): Promise<void> {
  await client.post('/api/system/update/rollback')
}

// ─── Mock API 实现 ──────────────────────────────────

async function mockGetSystemInfo(): Promise<SystemInfo> {
  await mockDelay(200)
  return {
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
  }
}

async function mockRunCodecTest(_force?: boolean): Promise<CodecTestResult> {
  await mockDelay(400)
  return {
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
  }
}

async function mockCheckUpdate(channel: string, _force = false): Promise<UpdateCheckResult> {
  await mockDelay(200)
  const prerelease = channel === 'prerelease'
  return {
    current: '0.6.2',
    latest: prerelease ? 'v0.6.3-dev.abc1234' : 'v0.6.3',
    has_update: true,
    tag: prerelease ? 'v0.6.3-dev.abc1234' : 'v0.6.3',
    prerelease,
    channel: prerelease ? 'prerelease' : 'stable',
    notes: '示例发布说明：修复若干问题。',
    asset_name: 'jianvideo-linux-amd64',
  }
}

async function mockApplyUpdate(_channel: string): Promise<void> {
  await mockDelay(300)
}

async function mockRollbackUpdate(): Promise<void> {
  await mockDelay(300)
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getSystemInfo(): Promise<SystemInfo> {
  return useMock ? mockGetSystemInfo() : realGetSystemInfo()
}

export function runCodecTest(force?: boolean): Promise<CodecTestResult> {
  return useMock ? mockRunCodecTest(force) : realRunCodecTest(force)
}

export function checkUpdate(channel: string, force = false): Promise<UpdateCheckResult> {
  return useMock ? mockCheckUpdate(channel, force) : realCheckUpdate(channel, force)
}

export function applyUpdate(channel: string): Promise<void> {
  return useMock ? mockApplyUpdate(channel) : realApplyUpdate(channel)
}

export function rollbackUpdate(): Promise<void> {
  return useMock ? mockRollbackUpdate() : realRollbackUpdate()
}

// pingHealth 探测服务是否在线，用于自更新/回滚重启后轮询恢复。
export async function pingHealth(): Promise<boolean> {
  if (useMock) return true
  try {
    const res = await client.get('/health', { timeout: 3000 })
    return res.status === 200
  } catch {
    return false
  }
}
