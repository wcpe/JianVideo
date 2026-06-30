import type { SettingsMap } from '@/types';
import client from './client';

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true';

// 已知设置键，与后端 internal/settings 常量保持一致。
export const SETTING_KEY_RECYCLE_BIN_PATHS = 'recycle_bin_paths';
export const SETTING_KEY_SCAN_INTERVAL = 'scan_interval';
// 更新频道：stable=正式版（拉正式 release）/ prerelease=测试版（拉最新预发布 dev）
export const SETTING_KEY_UPDATE_CHANNEL = 'update_channel';
// ffmpeg/ffprobe 可执行文件路径（FR-56），与后端 settings 常量一致
export const SETTING_KEY_FFMPEG_PATH = 'ffmpeg_path';
export const SETTING_KEY_FFPROBE_PATH = 'ffprobe_path';
// ImageMagick magick 可执行文件路径（FR-63），与后端 settings 常量一致
export const SETTING_KEY_MAGICK_PATH = 'magick_path';
// 后端出站网络代理 URL（FR-80），空=直连；与后端 settings 常量一致
export const SETTING_KEY_NETWORK_PROXY = 'network_proxy';
// 运行时调试日志开关（FR-110），"1"=开启详细日志、其余=安静；与后端 settings 常量一致
export const SETTING_KEY_DEBUG_LOG = 'debug_log';
// 目录浏览打开的标签列表（FR-151），JSON 数组：每项 {path,sort,displayMode}
export const SETTING_KEY_OPEN_TABS = 'open_tabs';
// 目录浏览上次浏览位置（FR-151），字符串路径，用于恢复时定位激活标签
export const SETTING_KEY_LAST_OPENED_PATH = 'last_opened_path';

function mockDelay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

// ─── 真实 API 实现 ──────────────────────────────────

async function realGetSettings(): Promise<SettingsMap> {
  const res = await client.get<{ settings: SettingsMap }>('/api/settings');
  return res.data.settings || {};
}

async function realUpdateSettings(values: SettingsMap): Promise<SettingsMap> {
  const res = await client.put<{ settings: SettingsMap }>('/api/settings', { settings: values });
  return res.data.settings || {};
}

// ─── Mock API 实现 ──────────────────────────────────

// mock 模式下的内存设置存储，支持读写往返。
const mockStore: SettingsMap = {
  [SETTING_KEY_SCAN_INTERVAL]: '3600',
  [SETTING_KEY_RECYCLE_BIN_PATHS]: '{"D":"D:/.recycle"}',
  [SETTING_KEY_UPDATE_CHANNEL]: 'stable',
};

async function mockGetSettings(): Promise<SettingsMap> {
  await mockDelay(150);
  return { ...mockStore };
}

async function mockUpdateSettings(values: SettingsMap): Promise<SettingsMap> {
  await mockDelay(150);
  Object.assign(mockStore, values);
  return { ...mockStore };
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getSettings(): Promise<SettingsMap> {
  return useMock ? mockGetSettings() : realGetSettings();
}

export function updateSettings(values: SettingsMap): Promise<SettingsMap> {
  return useMock ? mockUpdateSettings(values) : realUpdateSettings(values);
}
