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

async function realRunCodecTest(): Promise<CodecTestResult> {
  const res = await client.post<CodecTestResult>('/api/system/codec-test')
  return res.data
}

async function realCheckUpdate(channel: string): Promise<UpdateCheckResult> {
  const res = await client.get<UpdateCheckResult>('/api/system/update/check', { params: { channel } })
  return res.data
}

async function realApplyUpdate(channel: string): Promise<void> {
  await client.post('/api/system/update/apply', { channel })
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
          name: 'NVIDIA NVENC',
          device_type: 'cuda',
          h264_encoder: 'h264_nvenc',
          h265_encoder: 'hevc_nvenc',
          available: true,
        },
      ],
      preferred: 'h264_nvenc',
      intel_gpu: false,
      intel_gpu_detail: '',
      h264_supported: true,
      h265_supported: true,
      software_fallback: false,
    },
  }
}

async function mockRunCodecTest(): Promise<CodecTestResult> {
  await mockDelay(400)
  return {
    ffmpeg_available: true,
    results: [
      { encoder: 'libx264', family: 'software', codec: 'h264', compiled: true, tested_ok: true, detail: '' },
      { encoder: 'libx265', family: 'software', codec: 'h265', compiled: true, tested_ok: true, detail: '' },
      { encoder: 'h264_nvenc', family: 'nvenc', codec: 'h264', compiled: true, tested_ok: true, detail: '' },
      {
        encoder: 'h264_qsv',
        family: 'qsv',
        codec: 'h264',
        compiled: true,
        tested_ok: false,
        detail: '[h264_qsv @ 0x55] Error initializing an internal MFX session: unsupported (-3)',
      },
    ],
  }
}

async function mockCheckUpdate(channel: string): Promise<UpdateCheckResult> {
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

export function runCodecTest(): Promise<CodecTestResult> {
  return useMock ? mockRunCodecTest() : realRunCodecTest()
}

export function checkUpdate(channel: string): Promise<UpdateCheckResult> {
  return useMock ? mockCheckUpdate(channel) : realCheckUpdate(channel)
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
