import type { TranscodePreset, TranscodeTask, TranscodeCodec } from '@/types'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

// ─── 真实 API 实现 ──────────────────────────────────

import client from './client'
import type { AxiosError } from 'axios'

function getApiErrorMessage(err: unknown, fallback: string): string {
  const error = err as AxiosError<{ message?: string }>
  return error.response?.data?.message || (err instanceof Error ? err.message : fallback)
}

// 预设入参（建/改共用）
export interface PresetInput {
  name: string
  codec: TranscodeCodec
  width: number
  height: number
}

async function realListPresets(): Promise<TranscodePreset[]> {
  const res = await client.get<{ items: TranscodePreset[] }>('/api/transcode/presets')
  return res.data.items
}

async function realCreatePreset(input: PresetInput): Promise<TranscodePreset> {
  try {
    const res = await client.post<TranscodePreset>('/api/transcode/presets', input)
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '创建预设失败'), { cause: err })
  }
}

async function realUpdatePreset(id: number, input: PresetInput): Promise<TranscodePreset> {
  try {
    const res = await client.put<TranscodePreset>(`/api/transcode/presets/${id}`, input)
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '更新预设失败'), { cause: err })
  }
}

async function realDeletePreset(id: number): Promise<void> {
  await client.delete(`/api/transcode/presets/${id}`)
}

async function realEnqueueTask(mediaID: number, presetID: number): Promise<number> {
  try {
    const res = await client.post<{ status: string; task_id: number }>('/api/transcode/tasks', { media_id: mediaID, preset_id: presetID })
    return res.data.task_id
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '加入预生成队列失败'), { cause: err })
  }
}

async function realListTasks(status?: string): Promise<TranscodeTask[]> {
  const res = await client.get<{ tasks: TranscodeTask[] }>('/api/transcode/tasks', {
    params: status ? { status } : undefined,
  })
  return res.data.tasks
}

// ─── Mock API 实现 ──────────────────────────────────

const mockPresets: TranscodePreset[] = []
const mockTasks: TranscodeTask[] = []
let nextMockPresetId = 1
let nextMockTaskId = 1

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

const KNOWN_CODECS: TranscodeCodec[] = ['h264', 'h265', 'av1', 'vp9']

async function mockListPresets(): Promise<TranscodePreset[]> {
  await mockDelay(80)
  return [...mockPresets]
}

async function mockCreatePreset(input: PresetInput): Promise<TranscodePreset> {
  await mockDelay(80)
  const name = input.name.trim()
  if (!name) throw new Error('预设名不能为空')
  if (!KNOWN_CODECS.includes(input.codec)) throw new Error('不支持的目标编码')
  const now = new Date().toISOString()
  const preset: TranscodePreset = {
    id: nextMockPresetId++,
    name,
    codec: input.codec,
    width: input.width,
    height: input.height,
    created_at: now,
    updated_at: now,
  }
  mockPresets.unshift(preset)
  return preset
}

async function mockUpdatePreset(id: number, input: PresetInput): Promise<TranscodePreset> {
  await mockDelay(80)
  const p = mockPresets.find(x => x.id === id)
  if (!p) throw new Error('预设不存在')
  p.name = input.name.trim()
  p.codec = input.codec
  p.width = input.width
  p.height = input.height
  p.updated_at = new Date().toISOString()
  return { ...p }
}

async function mockDeletePreset(id: number): Promise<void> {
  await mockDelay(80)
  const idx = mockPresets.findIndex(x => x.id === id)
  if (idx !== -1) mockPresets.splice(idx, 1)
}

async function mockEnqueueTask(mediaID: number, presetID: number): Promise<number> {
  await mockDelay(80)
  const preset = mockPresets.find(p => p.id === presetID)
  if (!preset) throw new Error('预设不存在')
  const now = new Date().toISOString()
  const task: TranscodeTask = {
    id: nextMockTaskId++,
    media_id: mediaID,
    preset_id: presetID,
    codec: preset.codec,
    width: preset.width,
    height: preset.height,
    status: 'completed',
    error: '',
    created_at: now,
    started_at: now,
    completed_at: now,
  }
  mockTasks.unshift(task)
  return task.id
}

async function mockListTasks(status?: string): Promise<TranscodeTask[]> {
  await mockDelay(80)
  return status ? mockTasks.filter(t => t.status === status) : [...mockTasks]
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function listPresets() { return useMock ? mockListPresets() : realListPresets() }
export function createPreset(input: PresetInput) { return useMock ? mockCreatePreset(input) : realCreatePreset(input) }
export function updatePreset(id: number, input: PresetInput) { return useMock ? mockUpdatePreset(id, input) : realUpdatePreset(id, input) }
export function deletePreset(id: number) { return useMock ? mockDeletePreset(id) : realDeletePreset(id) }
export function enqueueTranscodeTask(mediaID: number, presetID: number) { return useMock ? mockEnqueueTask(mediaID, presetID) : realEnqueueTask(mediaID, presetID) }
export function listTranscodeTasks(status?: string) { return useMock ? mockListTasks(status) : realListTasks(status) }
