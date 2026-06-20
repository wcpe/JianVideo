import type { SystemInfo, CodecTestResult } from '@/types'
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

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getSystemInfo(): Promise<SystemInfo> {
  return useMock ? mockGetSystemInfo() : realGetSystemInfo()
}

export function runCodecTest(): Promise<CodecTestResult> {
  return useMock ? mockRunCodecTest() : realRunCodecTest()
}
