import type { SettingsMap } from '@/types'
import client from './client'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

// 已知设置键，与后端 internal/settings 常量保持一致。
export const SETTING_KEY_RECYCLE_BIN_PATHS = 'recycle_bin_paths'
export const SETTING_KEY_SCAN_INTERVAL = 'scan_interval'
// 更新频道：stable=正式版（拉正式 release）/ prerelease=测试版（拉最新预发布 dev）
export const SETTING_KEY_UPDATE_CHANNEL = 'update_channel'

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

// ─── 真实 API 实现 ──────────────────────────────────

async function realGetSettings(): Promise<SettingsMap> {
  const res = await client.get<{ settings: SettingsMap }>('/api/settings')
  return res.data.settings || {}
}

async function realUpdateSettings(values: SettingsMap): Promise<SettingsMap> {
  const res = await client.put<{ settings: SettingsMap }>('/api/settings', { settings: values })
  return res.data.settings || {}
}

// ─── Mock API 实现 ──────────────────────────────────

// mock 模式下的内存设置存储，支持读写往返。
const mockStore: SettingsMap = {
  [SETTING_KEY_SCAN_INTERVAL]: '3600',
  [SETTING_KEY_RECYCLE_BIN_PATHS]: '{"D":"D:/.recycle"}',
  [SETTING_KEY_UPDATE_CHANNEL]: 'stable',
}

async function mockGetSettings(): Promise<SettingsMap> {
  await mockDelay(150)
  return { ...mockStore }
}

async function mockUpdateSettings(values: SettingsMap): Promise<SettingsMap> {
  await mockDelay(150)
  Object.assign(mockStore, values)
  return { ...mockStore }
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getSettings(): Promise<SettingsMap> {
  return useMock ? mockGetSettings() : realGetSettings()
}

export function updateSettings(values: SettingsMap): Promise<SettingsMap> {
  return useMock ? mockUpdateSettings(values) : realUpdateSettings(values)
}
