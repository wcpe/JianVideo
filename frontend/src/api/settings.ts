import type { SettingDefinition, SettingsMap } from '@/types';
import client from './client';

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true';

// 已知设置键，与后端 internal/settings 常量保持一致。
export const SETTING_KEY_RECYCLE_BIN_PATHS = 'recycle_bin_paths';
export const SETTING_KEY_SCAN_INTERVAL = 'scan_interval';
// 更新频道：stable=正式版（拉正式 release）/ prerelease=测试版（拉最新预发布 dev）
export const SETTING_KEY_UPDATE_CHANNEL = 'update_channel';
// 转码目标编码优先级，JSON 数组；与后端 settings 常量一致
export const SETTING_KEY_TRANSCODE_CODEC_PRIORITY = 'transcode_codec_priority';
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
// Web 上传默认落盘目录（FR-149），须为已注册本地库目录或其子目录；与后端 settings 常量一致
export const SETTING_KEY_UPLOAD_TARGET_DIR = 'upload_target_dir';
// Web 上传命名规则（FR-149）：original=保留原样、date=按日期 YYYY/MM 整齐归档；与后端 settings 常量一致
export const SETTING_KEY_UPLOAD_NAMING_RULE = 'upload_naming_rule';
export const SETTING_SENSITIVE_DISPLAY_VALUE = '已设置';

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

async function realGetSettingDefinitions(): Promise<SettingDefinition[]> {
  const res = await client.get<{ definitions: SettingDefinition[] }>('/api/settings/definitions');
  return res.data.definitions || [];
}

// ─── Mock API 实现 ──────────────────────────────────

// mock 模式下的内存设置存储，支持读写往返。
const mockStore: SettingsMap = {
  [SETTING_KEY_SCAN_INTERVAL]: '3600',
  [SETTING_KEY_RECYCLE_BIN_PATHS]: '{"D":"D:/.recycle"}',
  [SETTING_KEY_UPDATE_CHANNEL]: 'stable',
  [SETTING_KEY_TRANSCODE_CODEC_PRIORITY]: '["h264"]',
};

const mockDefinitions: SettingDefinition[] = [
  {
    key: SETTING_KEY_SCAN_INTERVAL,
    label: '扫描周期',
    description: '定时扫描的间隔秒数，0 或留空表示关闭定时扫描。',
    layer: 'runtime',
    value_type: 'int',
    default_value: '0',
    sensitive: false,
    hot_apply: true,
    consumer: 'library.scheduler',
  },
  {
    key: SETTING_KEY_RECYCLE_BIN_PATHS,
    label: '回收站路径',
    description: '各盘符对应的回收站目录，保存为 JSON 对象。',
    layer: 'runtime',
    value_type: 'json',
    default_value: '{}',
    sensitive: false,
    hot_apply: true,
    consumer: 'library.recycle',
  },
  {
    key: SETTING_KEY_UPDATE_CHANNEL,
    label: '更新频道',
    description: '自更新检查使用的发布频道。',
    layer: 'runtime',
    value_type: 'enum',
    default_value: 'stable',
    sensitive: false,
    hot_apply: true,
    consumer: 'update',
    options: [
      { value: 'stable', label: '正式版' },
      { value: 'prerelease', label: '测试版' },
    ],
  },
  {
    key: SETTING_KEY_TRANSCODE_CODEC_PRIORITY,
    label: '转码编码优先级',
    description: '按优先顺序排列的目标编码 JSON 数组。',
    layer: 'runtime',
    value_type: 'json',
    default_value: '["h264"]',
    sensitive: false,
    hot_apply: true,
    consumer: 'transcoder',
  },
  {
    key: SETTING_KEY_FFMPEG_PATH,
    label: 'FFmpeg 路径',
    description: 'ffmpeg 可执行文件路径；留空时按自动发现结果使用。',
    layer: 'runtime',
    value_type: 'path',
    default_value: '',
    sensitive: false,
    hot_apply: true,
    consumer: 'transcoder',
  },
  {
    key: SETTING_KEY_FFPROBE_PATH,
    label: 'FFprobe 路径',
    description: 'ffprobe 可执行文件路径；留空时按自动发现结果使用。',
    layer: 'runtime',
    value_type: 'path',
    default_value: '',
    sensitive: false,
    hot_apply: true,
    consumer: 'library.transcoder',
  },
  {
    key: SETTING_KEY_MAGICK_PATH,
    label: 'Magick 路径',
    description: 'ImageMagick magick 可执行文件路径；留空时按自动发现结果使用。',
    layer: 'runtime',
    value_type: 'path',
    default_value: '',
    sensitive: false,
    hot_apply: true,
    consumer: 'library.imageconvert',
  },
  {
    key: SETTING_KEY_NETWORK_PROXY,
    label: '网络代理',
    description: '后端出站网络代理；支持 http、https、socks5、socks5h，凭据不回显。',
    layer: 'runtime',
    value_type: 'url',
    default_value: '',
    sensitive: true,
    hot_apply: true,
    consumer: 'netproxy',
  },
  {
    key: SETTING_KEY_DEBUG_LOG,
    label: '调试日志',
    description: '运行时详细日志开关。',
    layer: 'runtime',
    value_type: 'bool',
    default_value: '0',
    sensitive: false,
    hot_apply: true,
    consumer: 'dblog',
  },
  {
    key: SETTING_KEY_UPLOAD_TARGET_DIR,
    label: '默认上传位置',
    description: 'Web 上传缺省落盘目录，留空表示上传时必须指定。',
    layer: 'runtime',
    value_type: 'path',
    default_value: '',
    sensitive: false,
    hot_apply: true,
    consumer: 'library.upload',
  },
  {
    key: SETTING_KEY_UPLOAD_NAMING_RULE,
    label: '上传命名规则',
    description: 'Web 上传文件的默认归档规则。',
    layer: 'runtime',
    value_type: 'enum',
    default_value: 'original',
    sensitive: false,
    hot_apply: true,
    consumer: 'library.upload',
    options: [
      { value: 'original', label: '保留原样' },
      { value: 'date', label: '按日期归档' },
    ],
  },
  {
    key: SETTING_KEY_OPEN_TABS,
    label: '目录标签',
    description: '目录浏览打开标签的持久化快照。',
    layer: 'runtime',
    value_type: 'json',
    default_value: '[]',
    sensitive: false,
    hot_apply: true,
    consumer: 'browse-tabs',
  },
  {
    key: SETTING_KEY_LAST_OPENED_PATH,
    label: '上次浏览位置',
    description: '目录浏览最后打开的位置。',
    layer: 'runtime',
    value_type: 'path',
    default_value: '',
    sensitive: false,
    hot_apply: true,
    consumer: 'browse-tabs',
  },
  {
    key: 'server_port',
    label: '监听端口',
    description: '服务启动时确定的 HTTP 监听端口，运行期不可修改。',
    layer: 'startup',
    value_type: 'int',
    default_value: '',
    sensitive: false,
    hot_apply: false,
    consumer: 'config',
  },
  {
    key: 'db_path',
    label: '数据库路径',
    description: 'SQLite 数据库文件路径，运行期不可通过设置接口修改。',
    layer: 'startup',
    value_type: 'path',
    default_value: '',
    sensitive: false,
    hot_apply: false,
    consumer: 'config',
  },
  {
    key: 'jwt_secret',
    label: '会话密钥',
    description: 'JWT 签名密钥，只能通过启动环境配置。',
    layer: 'startup',
    value_type: 'string',
    default_value: '',
    sensitive: true,
    hot_apply: false,
    consumer: 'auth',
  },
];

async function mockGetSettings(): Promise<SettingsMap> {
  await mockDelay(150);
  return { ...mockStore };
}

async function mockUpdateSettings(values: SettingsMap): Promise<SettingsMap> {
  await mockDelay(150);
  Object.assign(mockStore, values);
  return { ...mockStore };
}

async function mockGetSettingDefinitions(): Promise<SettingDefinition[]> {
  await mockDelay(80);
  return [...mockDefinitions];
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getSettings(): Promise<SettingsMap> {
  return useMock ? mockGetSettings() : realGetSettings();
}

export function updateSettings(values: SettingsMap): Promise<SettingsMap> {
  return useMock ? mockUpdateSettings(values) : realUpdateSettings(values);
}

export function getSettingDefinitions(): Promise<SettingDefinition[]> {
  return useMock ? mockGetSettingDefinitions() : realGetSettingDefinitions();
}
