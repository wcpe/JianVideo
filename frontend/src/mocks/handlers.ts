import { http, HttpResponse, delay } from 'msw';
import { mockPaths, mockMediaFiles } from './data';
import type {
  LibraryPath,
  LibraryKind,
  MediaFile,
  MediaInference,
  MediaInferenceInput,
  MediaExtension,
  Album,
  Tag,
  ScanTask,
  Share,
  TranscodePreset,
  TranscodeTask,
  ShareResourceType,
  TranscodeCodec,
  AuditEvent,
  SettingDefinition,
  TaskItem,
  TaskStatus,
  MediaTypeRule,
  MediaTypesResponse,
  ToolSource,
  ToolStatus,
} from '@/types';

// 内存中的可变数据（支持增删）
let paths = [...mockPaths];
let mediaFiles = [...mockMediaFiles];
const mediaInferences = new Map<number, MediaInference>();
let mediaExtensions: MediaExtension[] = [];
let mediaTypeRuleOverrides: MediaTypeRule[] = [];
let nextPathId = Math.max(...paths.map((p) => p.id)) + 1;
let nextMediaId = Math.max(...mediaFiles.map((m) => m.id)) + 1;
let nextExtensionId = 1;

const mediaTypeDefinitions: MediaTypesResponse['types'] = [
  {
    type: 'video',
    name: '视频',
    description: '可播放、可转码的视频文件。',
    default_extensions: ['mp4', 'mkv', 'mov', 'avi', 'webm'],
    capabilities: ['scan', 'transcode', 'thumbnail', 'metadata'],
  },
  {
    type: 'image',
    name: '图片',
    description: '可生成缩略图的图片文件。',
    default_extensions: ['jpg', 'jpeg', 'png', 'webp', 'gif'],
    capabilities: ['scan', 'thumbnail', 'metadata'],
  },
];

function mediaTypeDefinition(type: string) {
  return mediaTypeDefinitions.find((item) => item.type === type);
}

function builtinMediaTypeRules(libraryID = 0): MediaTypeRule[] {
  return mediaTypeDefinitions.flatMap((type) =>
    type.default_extensions.map((extension) => ({
      id: `builtin-${type.type}-${extension}`,
      space_id: 'space-default',
      library_id: libraryID > 0 ? libraryID : null,
      type: type.type,
      extension,
      label: `${extension.toUpperCase()} ${type.name}`,
      description: `${extension} ${type.description}`,
      enabled: true,
      builtin: true,
      capabilities: type.capabilities,
    })),
  );
}

function mediaTypeRuleKey(rule: Pick<MediaTypeRule, 'library_id' | 'type' | 'extension'>): string {
  return `${rule.library_id ?? 0}:${rule.type}:${rule.extension}`;
}

function effectiveBuiltinMediaTypeRules(libraryID = 0): MediaTypeRule[] {
  const overrides = new Map(mediaTypeRuleOverrides.map((rule) => [mediaTypeRuleKey(rule), rule]));
  return builtinMediaTypeRules(libraryID).map((rule) => {
    return overrides.get(mediaTypeRuleKey(rule)) ?? rule;
  });
}

function mediaRuleFromExtension(ext: MediaExtension): MediaTypeRule {
  const definition = mediaTypeDefinition(ext.type);
  return {
    id: ext.id ?? 0,
    space_id: 'space-default',
    library_id: ext.library_id,
    type: ext.type,
    extension: ext.extension,
    label: ext.label || `${ext.extension.toUpperCase()} ${definition?.name ?? '媒体'}`,
    description: ext.description || `${ext.extension} ${definition?.description ?? '媒体规则'}`,
    enabled: ext.enabled ?? true,
    builtin: ext.builtin ?? Boolean(ext.is_builtin),
    capabilities: ext.capabilities ?? definition?.capabilities ?? ['scan'],
  };
}

// 运行期设置内存存储（支持读写往返）
const settingsStore: Record<string, string> = {
  scan_interval: '3600',
  recycle_bin_paths: '{"D":"D:/.recycle"}',
  ffmpeg_path: '',
  ffprobe_path: '',
  magick_path: '',
  network_proxy: '',
  debug_log: '0',
  upload_target_dir: '',
  upload_naming_rule: 'original',
  update_channel: 'stable',
  transcode_codec_priority: '["h264"]',
  transcode_hwaccel_mode: 'auto',
  transcode_hwaccel_fallback: '1',
};

const toolStatuses: ToolStatus[] = [
  {
    tool: 'ffmpeg',
    setting_key: 'ffmpeg_path',
    configured_path: '',
    installed: [],
  },
  {
    tool: 'ffprobe',
    setting_key: 'ffprobe_path',
    configured_path: '',
    installed: [],
  },
  {
    tool: 'magick',
    setting_key: 'magick_path',
    configured_path: '',
    installed: [],
  },
];

const toolSources: ToolSource[] = [
  {
    id: 'ffmpeg-mock',
    tool: 'ffmpeg',
    platform: 'windows',
    arch: 'amd64',
    version: 'mock-6.1.1',
    url: 'https://example.invalid/ffmpeg.zip',
    sha256: 'a'.repeat(64),
    size: 12_582_912,
    label: 'FFmpeg 示例源',
  },
  {
    id: 'ffprobe-mock',
    tool: 'ffprobe',
    platform: 'windows',
    arch: 'amd64',
    version: 'mock-6.1.1',
    url: 'https://example.invalid/ffprobe.zip',
    sha256: 'b'.repeat(64),
    size: 4_194_304,
    label: 'FFprobe 示例源',
  },
  {
    id: 'magick-mock',
    tool: 'magick',
    platform: 'windows',
    arch: 'amd64',
    version: 'mock-7.1.2',
    url: 'https://example.invalid/magick.zip',
    sha256: 'c'.repeat(64),
    size: 24_117_248,
    label: 'ImageMagick 示例源',
  },
];

const settingDefinitions: SettingDefinition[] = [
  {
    key: 'scan_interval',
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
    key: 'recycle_bin_paths',
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
    key: 'network_proxy',
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
    key: 'ffmpeg_path',
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
    key: 'ffprobe_path',
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
    key: 'magick_path',
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
    key: 'debug_log',
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
    key: 'upload_target_dir',
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
    key: 'upload_naming_rule',
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
    key: 'update_channel',
    label: '更新频道',
    description: '自更新检查使用的发布频道。',
    layer: 'runtime',
    value_type: 'enum',
    default_value: 'stable',
    sensitive: false,
    hot_apply: true,
    consumer: 'update',
  },
  {
    key: 'transcode_codec_priority',
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
    key: 'transcode_hwaccel_mode',
    label: '硬件转码策略',
    description: '默认硬件转码策略：自动、软件或指定硬件家族。',
    layer: 'runtime',
    value_type: 'enum',
    default_value: 'auto',
    sensitive: false,
    hot_apply: true,
    consumer: 'transcoder',
    options: [
      { value: 'auto', label: '自动' },
      { value: 'software', label: '软件' },
      { value: 'nvenc', label: 'NVIDIA NVENC' },
      { value: 'qsv', label: 'Intel QSV' },
      { value: 'amf', label: 'AMD AMF' },
      { value: 'vaapi', label: 'VAAPI' },
      { value: 'videotoolbox', label: 'Apple VideoToolbox' },
    ],
  },
  {
    key: 'transcode_hwaccel_fallback',
    label: '硬件失败软件回退',
    description: '指定硬件不可用或转码失败时是否自动改用软件编码。',
    layer: 'runtime',
    value_type: 'bool',
    default_value: '1',
    sensitive: false,
    hot_apply: true,
    consumer: 'transcoder',
  },
  {
    key: 'open_tabs',
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
    key: 'last_opened_path',
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

const writableSettingKeys = new Set(
  settingDefinitions
    .filter((definition) => definition.layer === 'runtime')
    .map((definition) => definition.key),
);

const auditEvents: AuditEvent[] = [
  {
    id: 3,
    scope: 'system',
    space_id: null,
    actor_type: 'system',
    actor_id: 'system',
    action: 'migration.succeeded',
    resource_type: 'migration',
    resource_id: '2026070801',
    before_json: null,
    after_json: { version: '2026070801' },
    metadata_json: { summary: '迁移完成' },
    request_id: 'req-migration-1',
    created_at: '2026-07-08T09:00:00Z',
  },
  {
    id: 2,
    scope: 'space',
    space_id: 'default',
    actor_type: 'user',
    actor_id: 'admin',
    action: 'media.deleted',
    resource_type: 'media',
    resource_id: '42',
    before_json: { file_name: 'example.mp4' },
    after_json: null,
    metadata_json: { summary: '移入回收站' },
    request_id: 'req-media-1',
    created_at: '2026-07-08T08:30:00Z',
  },
  {
    id: 1,
    scope: 'system',
    space_id: null,
    actor_type: 'user',
    actor_id: 'admin',
    action: 'settings.updated',
    resource_type: 'settings',
    resource_id: 'network_proxy',
    before_json: { network_proxy: '***' },
    after_json: { network_proxy: '***' },
    metadata_json: { summary: '更新网络代理' },
    request_id: 'req-settings-1',
    created_at: '2026-07-08T08:00:00Z',
  },
];

function pushSystemAuditEvent(event: Omit<AuditEvent, 'id' | 'created_at' | 'scope' | 'space_id'>): void {
  auditEvents.unshift({
    ...event,
    id: Math.max(0, ...auditEvents.map((item) => item.id)) + 1,
    scope: 'system',
    space_id: null,
    created_at: new Date().toISOString(),
  });
}

// 相册（FR-40）内存数据
let albums: Album[] = [];
let albumItems: { album_id: number; media_id: number }[] = [];
let nextAlbumId = 1;

// 标签（FR-41）
const tags: Tag[] = [];
let tagMappings: { tag_id: number; media_id: number }[] = [];
let nextTagId = 1;

// 软删除/回收站（FR-25）：被软删的媒体 ID 集合
const deletedMediaIds = new Set<number>();

// 扫描任务队列（FR-29）内存数据
const scanTasks: ScanTask[] = [];
let nextScanTaskId = 1;
let nextFileHashTaskId = 1;
const unifiedTasks: TaskItem[] = [
  {
    id: 'task-mock-scan-running',
    scope: 'space',
    space_id: 'space-default',
    type: 'library.scan',
    status: 'running',
    priority: 10,
    attempts: 0,
    max_attempts: 1,
    progress: 0.45,
    resource_type: 'library',
    resource_id: '1',
    error: null,
    created_at: '2026-07-08T08:00:00Z',
    updated_at: '2026-07-08T08:01:00Z',
  },
  {
    id: 'task-mock-transcode-failed',
    scope: 'space',
    space_id: 'space-default',
    type: 'transcode.hls',
    status: 'failed',
    priority: 5,
    attempts: 1,
    max_attempts: 3,
    progress: 0.32,
    resource_type: 'media',
    resource_id: '42',
    error: '编码器不可用',
    created_at: '2026-07-08T08:10:00Z',
    updated_at: '2026-07-08T08:11:00Z',
  },
  {
    id: 'task-mock-system-cleanup',
    scope: 'system',
    space_id: null,
    type: 'cache.cleanup',
    status: 'pending',
    priority: 1,
    attempts: 0,
    max_attempts: 1,
    progress: 0,
    resource_type: 'cache',
    resource_id: 'hls',
    error: null,
    created_at: '2026-07-08T08:20:00Z',
    updated_at: '2026-07-08T08:20:00Z',
  },
];

const cacheKinds = ['thumbnail', 'hls', 'image_proxy', 'cover', 'metadata_temp'] as const;
type MockCacheKind = (typeof cacheKinds)[number];
type MockCacheRow = {
  kind: MockCacheKind;
  size_bytes: number;
  file_count: number;
  asset_count: number;
};

const cacheRows: Record<MockCacheKind, MockCacheRow> = {
  thumbnail: { kind: 'thumbnail', size_bytes: 280_000_000, file_count: 860, asset_count: 860 },
  hls: { kind: 'hls', size_bytes: 1_920_000_000, file_count: 4180, asset_count: 12 },
  image_proxy: { kind: 'image_proxy', size_bytes: 96_000_000, file_count: 44, asset_count: 44 },
  cover: { kind: 'cover', size_bytes: 32_000_000, file_count: 26, asset_count: 26 },
  metadata_temp: { kind: 'metadata_temp', size_bytes: 8_000_000, file_count: 18, asset_count: 18 },
};
let nextCacheTaskID = 10_000;
const cacheInventoryPolls = new Map<string, number>();

// 公开分享（FR-43/FR-78）内存数据
let shares: Share[] = [];
let nextShareTokenSeq = 1;

// 转码预设与预生成任务（FR-77）内存数据
let transcodePresets: TranscodePreset[] = [];
const transcodeTasks: TranscodeTask[] = [];
let nextPresetId = 1;
let nextTranscodeTaskId = 1;
// 支持的目标编码（与 api 层 KNOWN_CODECS 对齐）
const KNOWN_TRANSCODE_CODECS: TranscodeCodec[] = ['h264', 'h265', 'av1', 'vp9'];

function addUnifiedTask(task: TaskItem) {
  unifiedTasks.unshift(task);
}

function pollCacheInventoryTask(task: TaskItem): void {
  if (task.type !== 'cache.inventory' || task.status !== 'running') return;
  const polls = (cacheInventoryPolls.get(task.id) ?? 0) + 1;
  cacheInventoryPolls.set(task.id, polls);
  if (polls < 2) return;
  task.status = 'succeeded';
  task.progress = 1;
  task.updated_at = new Date().toISOString();
  task.finished_at = task.updated_at;
  cacheInventoryPolls.delete(task.id);
}

function currentSpaceID(request: Request): string {
  const url = new URL(request.url);
  return (
    url.searchParams.get('space_id') ||
    request.headers.get('X-JianVideo-Space-Id') ||
    'space-default'
  );
}

function visibleUnifiedTasks(request: Request): TaskItem[] {
  const url = new URL(request.url);
  const scope = (url.searchParams.get('scope') || 'space') as 'space' | 'system';
  const type = url.searchParams.get('type') || '';
  const status = (url.searchParams.get('status') || '') as TaskStatus | '';
  const resourceType = url.searchParams.get('resource_type') || '';
  const resourceID = url.searchParams.get('resource_id') || '';
  const spaceID = currentSpaceID(request);
  return unifiedTasks.filter((task) => {
    if (task.scope !== scope) return false;
    if (scope === 'space' && task.space_id !== spaceID) return false;
    if (type && task.type !== type) return false;
    if (status && task.status !== status) return false;
    if (resourceType && task.resource_type !== resourceType) return false;
    if (resourceID && task.resource_id !== resourceID) return false;
    return true;
  });
}

function countTasksBy<T extends string>(
  items: TaskItem[],
  keyOf: (item: TaskItem) => T,
): Record<T, number> {
  return items.reduce<Record<T, number>>(
    (result, item) => {
      const key = keyOf(item);
      result[key] = (result[key] ?? 0) + 1;
      return result;
    },
    {} as Record<T, number>,
  );
}

function countTaskStatuses(items: TaskItem[]): Record<TaskStatus, number> {
  const counts: Record<TaskStatus, number> = {
    pending: 0,
    running: 0,
    succeeded: 0,
    failed: 0,
    canceled: 0,
  };
  for (const item of items) {
    counts[item.status] += 1;
  }
  return counts;
}

export const handlers = [
  // ─── 认证 ───────────────────────────────────────────

  http.post('*/api/auth/login', async ({ request }) => {
    await delay(300);
    const body = (await request.json()) as { username: string; password: string };
    if (body.username === 'admin' && body.password === 'admin') {
      return HttpResponse.json(
        { username: 'admin' },
        {
          headers: {
            'Set-Cookie': 'auth_token=mock_jwt; Path=/; HttpOnly; Secure; Max-Age=259200',
          },
        },
      );
    }
    return HttpResponse.json(
      { code: 'INVALID_CREDENTIALS', message: '用户名或密码错误' },
      { status: 401 },
    );
  }),

  http.post('*/api/auth/logout', async () => {
    await delay(100);
    return new HttpResponse(null, {
      headers: { 'Set-Cookie': 'auth_token=; Path=/; Max-Age=0' },
    });
  }),

  http.get('*/api/me', async () => {
    await delay(100);
    return HttpResponse.json({ username: 'admin' });
  }),

  // 首次初始化（FR-109）：默认已初始化（admin 存在），不打扰常规用例
  http.get('*/api/auth/setup-status', async () => {
    await delay(50);
    return HttpResponse.json({ needs_setup: false });
  }),

  http.post('*/api/auth/setup', async ({ request }) => {
    await delay(200);
    const body = (await request.json()) as { username: string; password: string };
    return HttpResponse.json(
      { username: body.username },
      {
        headers: { 'Set-Cookie': 'auth_token=mock_jwt; Path=/; HttpOnly; Secure; Max-Age=259200' },
      },
    );
  }),

  // 修改密码（FR-108）：当前密码为 admin 视为正确，否则 401
  http.post('*/api/me/password', async ({ request }) => {
    await delay(200);
    const body = (await request.json()) as { old_password: string; new_password: string };
    if (body.old_password !== 'admin') {
      return HttpResponse.json(
        { code: 'WRONG_PASSWORD', message: '当前密码错误' },
        { status: 401 },
      );
    }
    return new HttpResponse(null, { status: 204 });
  }),

  // ─── 媒体库目录 ─────────────────────────────────────

  http.get('*/api/library/paths', async () => {
    await delay(200);
    // 为每个库附带已索引媒体数量，与真实接口字段一致
    const items = paths.map((p) => ({
      ...p,
      media_count: mediaFiles.filter((m) => m.library_id === p.id).length,
    }));
    return HttpResponse.json({ items });
  }),

  http.get('*/api/library/kinds', async () => {
    await delay(80);
    return HttpResponse.json({
      items: [
        {
          kind: 'movie',
          name: '电影',
          description: '面向电影与长片，后续用于标题与年份解析。',
          naming_hint: '片名 (年份)/片名.ext',
          scan_strategy: '按文件与上级目录识别单片资源',
        },
        {
          kind: 'series',
          name: '剧集',
          description: '面向电视剧、番剧与课程，后续用于季集入口。',
          naming_hint: '剧名/Season 01/剧名 S01E01.ext',
          scan_strategy: '保留季集解析上下文',
        },
        {
          kind: 'home_video',
          name: '家庭录像',
          description: '面向家庭影像、相机视频和生活记录。',
          naming_hint: '日期_地点_事件.ext',
          scan_strategy: '优先保留拍摄时间与原始文件名',
        },
        {
          kind: 'mixed',
          name: '混合',
          description: '兼容旧库与混合内容，不套用专门影视规则。',
          naming_hint: '保持现有目录与文件名',
          scan_strategy: '使用通用扫描策略',
        },
      ],
    });
  }),

  http.post('*/api/library/paths', async ({ request }) => {
    await delay(300);
    const body = (await request.json()) as {
      library_kind?: LibraryKind;
      label: string;
      path: string;
      type: string;
    };
    const newPath: LibraryPath = {
      id: nextPathId++,
      path: body.path,
      type: body.type || 'local',
      library_kind: body.library_kind || 'mixed',
      library_profile_json: '{}',
      label: body.label || body.path,
      enabled: true,
      created_at: new Date().toISOString(),
    };
    paths.push(newPath);
    return HttpResponse.json(newPath, { status: 201 });
  }),

  http.put('*/api/library/paths/:id', async ({ request, params }) => {
    await delay(120);
    const id = Number(params.id);
    const path = paths.find((p) => p.id === id);
    if (!path) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体库不存在' }, { status: 404 });
    }
    const body = (await request.json()) as {
      enabled?: boolean;
      label?: string;
      library_kind?: LibraryKind;
    };
    if (body.label !== undefined) path.label = body.label.trim();
    if (body.enabled !== undefined) path.enabled = body.enabled;
    if (body.library_kind !== undefined) path.library_kind = body.library_kind;
    return HttpResponse.json(path);
  }),

  http.delete('*/api/library/paths/:id', async ({ params }) => {
    await delay(200);
    const id = Number(params.id);
    paths = paths.filter((p) => p.id !== id);
    // 同时删除关联的媒体文件、后缀和规则覆盖
    mediaFiles = mediaFiles.filter((m) => m.library_id !== id);
    mediaExtensions = mediaExtensions.filter((ext) => ext.library_id !== id);
    mediaTypeRuleOverrides = mediaTypeRuleOverrides.filter((rule) => rule.library_id !== id);
    return new HttpResponse(null, { status: 204 });
  }),

  // ─── 媒体文件 ───────────────────────────────────────

  http.get('*/api/library/media', async ({ request }) => {
    await delay(300);
    const url = new URL(request.url);
    const page = Number(url.searchParams.get('page') || '1');
    const pageSize = Number(url.searchParams.get('page_size') || '20');
    const search = url.searchParams.get('search') || '';
    const sort = url.searchParams.get('sort') || '';
    const favorite = url.searchParams.get('favorite');
    const tagID = Number(url.searchParams.get('tag_id') || '0');

    // 常规列表排除已软删项（FR-25）
    let items = mediaFiles.filter((m) => !deletedMediaIds.has(m.id));
    if (search) {
      items = items.filter((m) => m.file_name.toLowerCase().includes(search.toLowerCase()));
    }
    if (favorite === 'true' || favorite === '1') {
      items = items.filter((m) => m.favorite);
    }
    if (tagID > 0) {
      const ids = new Set(tagMappings.filter((tm) => tm.tag_id === tagID).map((tm) => tm.media_id));
      items = items.filter((m) => ids.has(m.id));
    }
    if (sort === 'time_desc') {
      items.sort((a, b) => b.added_at.localeCompare(a.added_at));
    }

    const total = items.length;
    const start = (page - 1) * pageSize;
    const paged = items.slice(start, start + pageSize);

    return HttpResponse.json({ items: paged, total, page, page_size: pageSize });
  }),

  // ─── 收藏与标签（FR-41）──────────────────────────────

  // 真实文件名改名（FR-30）：仅模拟更新 file_name，不动 display_name
  http.put('*/api/library/media/:id/rename', async ({ request, params }) => {
    await delay(100);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    const body = (await request.json()) as { new_name: string };
    const newName = (body.new_name || '').trim();
    if (!newName || /[/\\]/.test(newName) || newName === '.' || newName === '..') {
      return HttpResponse.json(
        { code: 'RENAME_REJECTED', message: '新文件名不合法' },
        { status: 400 },
      );
    }
    file.file_name = newName;
    return HttpResponse.json(file);
  }),

  // 显示名修改（FR-30）：仅更新库内 display_name，不动磁盘真实文件名
  http.put('*/api/library/media/:id/display-name', async ({ request, params }) => {
    await delay(100);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    const body = (await request.json()) as { display_name: string };
    file.display_name = (body.display_name || '').trim();
    return HttpResponse.json(file);
  }),

  http.put('*/api/library/media/:id/favorite', async ({ request, params }) => {
    await delay(100);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    const body = (await request.json()) as { favorite: boolean };
    file.favorite = body.favorite;
    return HttpResponse.json(file);
  }),

  http.get('*/api/library/tags', async () => {
    await delay(100);
    return HttpResponse.json({ items: [...tags].sort((a, b) => a.name.localeCompare(b.name)) });
  }),

  http.post('*/api/library/tags', async ({ request }) => {
    await delay(100);
    const body = (await request.json()) as { name: string };
    const name = (body.name || '').trim();
    if (!name) {
      return HttpResponse.json({ code: 'INVALID_TAG', message: '标签名不能为空' }, { status: 400 });
    }
    let tag = tags.find((t) => t.name === name);
    if (!tag) {
      tag = { id: nextTagId++, name, created_at: new Date().toISOString() };
      tags.push(tag);
    }
    return HttpResponse.json(tag, { status: 201 });
  }),

  http.get('*/api/library/media/:id/tags', async ({ params }) => {
    await delay(100);
    const id = Number(params.id);
    const ids = new Set(tagMappings.filter((tm) => tm.media_id === id).map((tm) => tm.tag_id));
    return HttpResponse.json({
      items: tags.filter((t) => ids.has(t.id)).sort((a, b) => a.name.localeCompare(b.name)),
    });
  }),

  http.post('*/api/library/media/:id/tags', async ({ request, params }) => {
    await delay(100);
    const mediaID = Number(params.id);
    const body = (await request.json()) as { tag_id?: number; name?: string };
    let tag: Tag | undefined;
    if (body.tag_id) {
      tag = tags.find((t) => t.id === body.tag_id);
    } else if (body.name) {
      const name = body.name.trim();
      if (!name)
        return HttpResponse.json(
          { code: 'INVALID_TAG', message: '标签名不能为空' },
          { status: 400 },
        );
      tag = tags.find((t) => t.name === name);
      if (!tag) {
        tag = { id: nextTagId++, name, created_at: new Date().toISOString() };
        tags.push(tag);
      }
    }
    if (!tag)
      return HttpResponse.json({ code: 'ADD_TAG_FAILED', message: '标签不存在' }, { status: 400 });
    if (!tagMappings.some((tm) => tm.tag_id === tag!.id && tm.media_id === mediaID)) {
      tagMappings.push({ tag_id: tag.id, media_id: mediaID });
    }
    return HttpResponse.json(tag, { status: 201 });
  }),

  http.delete('*/api/library/media/:id/tags/:tagId', async ({ params }) => {
    await delay(100);
    const mediaID = Number(params.id);
    const tagID = Number(params.tagId);
    tagMappings = tagMappings.filter((tm) => !(tm.tag_id === tagID && tm.media_id === mediaID));
    return new HttpResponse(null, { status: 204 });
  }),

  // ─── 续播与观看状态（FR-44）─────────────────────────

  http.get('*/api/library/continue-watching', async ({ request }) => {
    await delay(100);
    const limit = Number(new URL(request.url).searchParams.get('limit') || '12');
    const items = mediaFiles
      .filter((m) => (m.last_position ?? 0) > 0 && !m.watched)
      .sort((a, b) => (b.last_watched_at ?? '').localeCompare(a.last_watched_at ?? ''))
      .slice(0, limit);
    return HttpResponse.json({ items });
  }),

  // ─── 最近查看（FR-120）────────────────────────────────

  // 记录媒体查看：把 last_viewed_at 置为当前时间（不存在 → 404）
  http.put('*/api/library/media/:id/viewed', async ({ params }) => {
    await delay(80);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    file.last_viewed_at = new Date().toISOString();
    return HttpResponse.json({ ok: true });
  }),

  // 最近查看列表：last_viewed_at 非空、未软删，按 last_viewed_at 倒序
  http.get('*/api/library/recently-viewed', async ({ request }) => {
    await delay(100);
    const limit = Number(new URL(request.url).searchParams.get('limit') || '12');
    const items = mediaFiles
      .filter((m) => !deletedMediaIds.has(m.id) && !!m.last_viewed_at)
      .sort((a, b) => (b.last_viewed_at ?? '').localeCompare(a.last_viewed_at ?? ''))
      .slice(0, limit);
    return HttpResponse.json({ items });
  }),

  // 编码协商（FR-53）：默认返回 h264/TS 描述符，使既有播放流程沿用 master 探测；
  // 需要 fMP4 路径的用例在测试中用 server.use 覆盖。
  http.post('*/api/play/:id/negotiate', async ({ params }) => {
    await delay(50);
    const id = Number(params.id);
    return HttpResponse.json({ codec: 'h264', path: 'ts', url: `/api/play/hls/${id}/master` });
  }),

  // HLS 播放列表（FR-124 补全）：协商后播放器探测该 master URL。
  // 测试环境（jsdom 无 MSE）无法真正播放 HLS，显式让 master 探测失败，
  // 触发播放器回退到 /api/play/:id/stream（既有用例验证此回退路径），同时消除未处理请求。
  http.get('*/api/play/hls/:id/master', async () => {
    await delay(50);
    return HttpResponse.error();
  }),

  http.put('*/api/play/:id/position', async ({ request, params }) => {
    await delay(100);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    const body = (await request.json()) as { position: number };
    file.last_position = body.position < 0 ? 0 : body.position;
    file.last_watched_at = new Date().toISOString();
    return HttpResponse.json(file);
  }),

  http.put('*/api/play/:id/watched', async ({ params }) => {
    await delay(100);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    file.watched = true;
    file.last_position = 0;
    file.last_watched_at = new Date().toISOString();
    return HttpResponse.json(file);
  }),

  http.get('*/api/library/media/:id/raw', async ({ params }) => {
    await delay(100);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    return new HttpResponse('', { headers: { 'Content-Type': `image/${file.format}` } });
  }),

  // 下载原文件（FR-42）：以附件形式回传原始文件
  http.get('*/api/library/media/:id/download', async ({ params }) => {
    await delay(100);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    return new HttpResponse('mock-file-bytes', {
      headers: {
        'Content-Disposition': `attachment; filename*=UTF-8''${encodeURIComponent(file.file_name)}`,
      },
    });
  }),

  http.get('*/api/library/media/:id', async ({ params }) => {
    await delay(200);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    return HttpResponse.json(file);
  }),

  http.get('*/api/library/media/:id/inference', async ({ params }) => {
    await delay(80);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    return HttpResponse.json({ inference: mediaInferences.get(id) ?? null });
  }),

  http.put('*/api/library/media/:id/inference', async ({ request, params }) => {
    await delay(100);
    const id = Number(params.id);
    const file = mediaFiles.find((m) => m.id === id);
    if (!file) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '媒体文件不存在' }, { status: 404 });
    }
    const body = (await request.json()) as MediaInferenceInput;
    const now = new Date().toISOString();
    const inference: MediaInference = {
      id,
      media_id: id,
      space_id: 'space-default',
      kind: body.kind ?? 'mixed',
      title: body.title.trim(),
      year: body.year ?? 0,
      season: body.season ?? 0,
      episode: body.episode ?? 0,
      episode_title: body.episode_title?.trim() ?? '',
      confidence: 1,
      source: 'manual',
      rule_version: 'fr2-031-v1',
      manual: true,
      created_at: mediaInferences.get(id)?.created_at ?? now,
      updated_at: now,
    };
    mediaInferences.set(id, inference);
    return HttpResponse.json(inference);
  }),

  http.post('*/api/library/inference/backfill', async ({ request }) => {
    await delay(120);
    const body = (await request.json()) as { library_id?: number };
    const now = new Date().toISOString();
    const taskID = String(Date.now());
    const task: TaskItem = {
      id: taskID,
      scope: 'space',
      space_id: currentSpaceID(request),
      type: 'library.inference.backfill',
      status: 'pending',
      priority: 0,
      attempts: 0,
      max_attempts: 1,
      progress: 0,
      resource_type: 'library',
      resource_id: String(body.library_id ?? 0),
      error: null,
      created_at: now,
      updated_at: now,
    };
    addUnifiedTask(task);
    setTimeout(() => {
      const finishedAt = new Date().toISOString();
      task.status = 'succeeded';
      task.progress = 1;
      task.updated_at = finishedAt;
      task.started_at = now;
      task.finished_at = finishedAt;
    }, 150);
    return HttpResponse.json({ status: 'pending', task_id: Number(taskID) }, { status: 202 });
  }),

  http.get('*/api/media-types', async ({ request }) => {
    await delay(100);
    const libraryID = Number(new URL(request.url).searchParams.get('library_id') || '0');
    return HttpResponse.json({
      types: mediaTypeDefinitions,
      rules: [
        ...effectiveBuiltinMediaTypeRules(libraryID),
        ...mediaExtensions
          .filter((ext) => !libraryID || ext.library_id === libraryID)
          .map((ext) => mediaRuleFromExtension(ext)),
      ],
    });
  }),

  http.post('*/api/media-types/rules', async ({ request }) => {
    await delay(100);
    const body = (await request.json()) as {
      library_id?: number;
      extension: string;
      type: 'image' | 'video';
      label?: string;
      description?: string;
      enabled?: boolean;
    };
    const extension = body.extension.trim().toLowerCase().replace(/^\./, '');
    if (!extension) {
      return HttpResponse.json(
        { code: 'INVALID_EXTENSION', message: '后缀格式不支持' },
        { status: 400 },
      );
    }
    const libraryID = body.library_id ?? 0;
    let ext = mediaExtensions.find(
      (item) =>
        item.library_id === libraryID && item.extension === extension && item.type === body.type,
    );
    if (!ext) {
      ext = {
        id: nextExtensionId++,
        library_id: libraryID,
        extension,
        type: body.type,
        is_builtin: 0,
        builtin: false,
        enabled: body.enabled ?? true,
        label: body.label,
        description: body.description,
        created_at: new Date().toISOString(),
      };
      mediaExtensions.push(ext);
    }
    return HttpResponse.json(mediaRuleFromExtension(ext), { status: 201 });
  }),

  http.put('*/api/media-types/rules/:id', async ({ params, request }) => {
    await delay(100);
    const id = String(params.id);
    const body = (await request.json()) as {
      library_id?: number;
      enabled?: boolean;
      label?: string;
      description?: string;
    };
    const ext = mediaExtensions.find((item) => String(item.id) === id);
    if (ext) {
      if (body.enabled !== undefined) ext.enabled = body.enabled;
      if (body.label !== undefined) ext.label = body.label.trim();
      if (body.description !== undefined) ext.description = body.description.trim();
      return HttpResponse.json(mediaRuleFromExtension(ext));
    }
    const builtin = builtinMediaTypeRules(body.library_id).find((rule) => String(rule.id) === id);
    if (!builtin) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '规则不存在' }, { status: 404 });
    }
    const updated = {
      ...builtin,
      enabled: body.enabled ?? builtin.enabled,
      label: body.label ?? builtin.label,
      description: body.description ?? builtin.description,
    };
    const key = mediaTypeRuleKey(updated);
    const idx = mediaTypeRuleOverrides.findIndex((rule) => mediaTypeRuleKey(rule) === key);
    if (idx >= 0) {
      mediaTypeRuleOverrides[idx] = updated;
    } else {
      mediaTypeRuleOverrides.push(updated);
    }
    return HttpResponse.json(updated);
  }),

  http.delete('*/api/media-types/rules/:id', async ({ params }) => {
    await delay(100);
    const id = String(params.id);
    const idx = mediaExtensions.findIndex((ext) => String(ext.id) === id);
    if (idx === -1) {
      return HttpResponse.json(
        { code: 'DELETE_RULE_FAILED', message: '自定义后缀不存在' },
        { status: 400 },
      );
    }
    mediaExtensions.splice(idx, 1);
    return new HttpResponse(null, { status: 204 });
  }),

  http.get('*/api/library/extensions', async ({ request }) => {
    await delay(100);
    const libraryID = Number(new URL(request.url).searchParams.get('library_id') || '0');
    return HttpResponse.json({
      items: mediaExtensions.filter((ext) => ext.library_id === libraryID),
    });
  }),

  http.post('*/api/library/extensions', async ({ request }) => {
    await delay(100);
    const body = (await request.json()) as {
      library_id: number;
      extension: string;
      type: 'image' | 'video';
    };
    const extension = body.extension.trim().toLowerCase().replace(/^\./, '');
    if (!extension) {
      return HttpResponse.json(
        { code: 'INVALID_EXTENSION', message: '后缀格式不支持' },
        { status: 400 },
      );
    }
    if (
      !mediaExtensions.some(
        (ext) => ext.library_id === body.library_id && ext.extension === extension,
      )
    ) {
      mediaExtensions.push({
        id: nextExtensionId++,
        library_id: body.library_id,
        extension,
        type: body.type,
        is_builtin: 0,
        created_at: new Date().toISOString(),
      });
    }
    return new HttpResponse(null, { status: 201 });
  }),

  // 删除自定义后缀（FR-64）：内置不可删、删不存在返回 400
  http.delete('*/api/library/extensions', async ({ request }) => {
    await delay(100);
    const url = new URL(request.url);
    const libraryID = Number(url.searchParams.get('library_id') || '0');
    const extension = (url.searchParams.get('extension') || '')
      .trim()
      .toLowerCase()
      .replace(/^\./, '');
    const idx = mediaExtensions.findIndex(
      (ext) => ext.library_id === libraryID && ext.extension === extension && ext.is_builtin === 0,
    );
    if (idx === -1) {
      return HttpResponse.json(
        { code: 'DELETE_EXTENSION_FAILED', message: '自定义后缀不存在' },
        { status: 400 },
      );
    }
    mediaExtensions.splice(idx, 1);
    return new HttpResponse(null, { status: 204 });
  }),

  http.delete('*/api/library/media/:id', async ({ params }) => {
    await delay(200);
    const id = Number(params.id);
    // 软删除（FR-25）：仅标记，不从内存数据移除
    if (mediaFiles.some((m) => m.id === id)) deletedMediaIds.add(id);
    return new HttpResponse(null, { status: 204 });
  }),

  // 批量软删（FR-69）：把存在且未软删的 id 批量加入回收站，返回实际软删条数
  http.post('*/api/library/media/batch-delete', async ({ request }) => {
    await delay(200);
    const body = (await request.json()) as { ids?: number[] };
    let deleted = 0;
    for (const id of body.ids ?? []) {
      if (mediaFiles.some((m) => m.id === id) && !deletedMediaIds.has(id)) {
        deletedMediaIds.add(id);
        deleted++;
      }
    }
    return HttpResponse.json({ deleted });
  }),

  // ─── 软删除与回收站（FR-25）──────────────────────────

  http.get('*/api/library/recycle', async () => {
    await delay(200);
    const items = mediaFiles.filter((m) => deletedMediaIds.has(m.id));
    return HttpResponse.json({ items });
  }),

  http.post('*/api/library/media/:id/restore', async ({ params }) => {
    await delay(200);
    const id = Number(params.id);
    if (!deletedMediaIds.has(id)) {
      return HttpResponse.json(
        { code: 'NOT_FOUND', message: '回收站中不存在该媒体文件' },
        { status: 404 },
      );
    }
    deletedMediaIds.delete(id);
    return new HttpResponse(null, { status: 204 });
  }),

  // 回收站清理（FR-26）：把全部软删项移出（模拟移到回收站目录 + 删记录）
  http.post('*/api/library/recycle/cleanup', async () => {
    await delay(200);
    let moved = 0;
    for (const id of deletedMediaIds) {
      mediaFiles = mediaFiles.filter((m) => m.id !== id);
      moved++;
    }
    deletedMediaIds.clear();
    return HttpResponse.json({ moved, failed: 0 });
  }),

  // ─── 目录浏览 ─────────────────────────────────────────

  http.get('*/api/library/browse', async ({ request }) => {
    await delay(200);
    const url = new URL(request.url);
    const parentPath = url.searchParams.get('parent_path') || '__root__';
    const sort = url.searchParams.get('sort') || 'name';
    const alive = mediaFiles.filter((m) => !deletedMediaIds.has(m.id));

    // 真实路径树根（FR-121）：各启用库推导卷根、去重排序作为顶层目录项（不带 library_id）。
    if (parentPath === '__root__') {
      const volRoot = (raw: string): string => {
        const p = raw.replace(/\\/g, '/');
        if (p.startsWith('//')) {
          const seg = p.slice(2).split('/').filter(Boolean);
          return seg.length >= 2 ? `//${seg[0]}/${seg[1]}` : p;
        }
        const m = p.match(/^([A-Za-z]:)/);
        return m ? m[1] : p.split('/').filter(Boolean)[0] || p;
      };
      const roots = Array.from(
        new Set(paths.filter((p) => p.enabled).map((p) => volRoot(p.path))),
      ).sort();
      return HttpResponse.json({
        breadcrumbs: [{ name: '全部', path: '__root__' }],
        directories: roots.map((r) => ({ name: r, path: r })),
        files: [],
      });
    }

    // 浏览真实路径 P（FR-121）：跨所有库按前缀合并，不依赖 library_id。
    const prefix = parentPath.replace(/\\/g, '/') + '/';
    const dirSet = new Set<string>();
    const files: MediaFile[] = [];
    for (const f of alive) {
      const fp = f.file_path.replace(/\\/g, '/');
      if (!fp.startsWith(prefix)) continue;
      const rel = fp.slice(prefix.length);
      const slashIdx = rel.indexOf('/');
      if (slashIdx !== -1) dirSet.add(rel.substring(0, slashIdx));
      else files.push(f);
    }

    // 面包屑：按分隔符拆段累进，Windows 盘符不加前导斜杠（`D:/...`）。
    const cleanPath = parentPath.replace(/\\/g, '/').replace(/\/+$/g, '');
    const parts = cleanPath.split('/').filter(Boolean);
    const breadcrumbs: { name: string; path: string }[] = [];
    let current = '';
    for (const seg of parts) {
      current = current === '' ? seg : `${current}/${seg}`;
      breadcrumbs.push({ name: seg, path: current });
    }
    if (breadcrumbs.length === 0)
      breadcrumbs.push({ name: cleanPath || '/', path: cleanPath || '/' });

    // 服务端排序（FR-121）：目录恒在前、按名；文件按 sort 升序。
    const sorted = [...files].sort((a, b) => {
      if (sort === 'size') return a.file_size - b.file_size;
      if (sort === 'type')
        return (
          (a.format || '').localeCompare(b.format || '') || a.file_name.localeCompare(b.file_name)
        );
      if (sort === 'time') return (a.modified_at || '').localeCompare(b.modified_at || '');
      return a.file_name.localeCompare(b.file_name);
    });

    return HttpResponse.json({
      breadcrumbs,
      directories: Array.from(dirSet)
        .sort()
        .map((name) => ({ name, path: prefix + name })),
      files: sorted,
    });
  }),

  // ─── 字幕 ─────────────────────────────────────────────

  http.get('*/api/play/:id/subtitles', async () => {
    await delay(150);
    return HttpResponse.json({
      tracks: [
        { index: 0, file_name: '电影名.srt', format: 'srt', url: '/api/play/1/subtitles/0' },
        { index: 1, file_name: '电影名.ass', format: 'ass', url: '/api/play/1/subtitles/1' },
      ],
    });
  }),

  http.get('*/api/play/:id/subtitles/:index', async () => {
    await delay(100);
    return new HttpResponse(
      'WEBVTT\n\n1\n00:00:01.000 --> 00:00:03.000\n这是第一条测试字幕\n\n2\n00:00:04.000 --> 00:00:06.000\n这是第二条测试字幕\n',
      { headers: { 'Content-Type': 'text/vtt' } },
    );
  }),

  // ─── SMB 凭据 ──────────────────────────────────────────

  http.post('*/api/smb/credentials', async () => {
    await delay(200);
    return new HttpResponse(null, { status: 204 });
  }),

  // ─── 系统诊断 ──────────────────────────────────────────

  http.get('*/api/system/info', async () => {
    await delay(150);
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
    });
  }),

  http.post('*/api/system/codec-test', async ({ request }) => {
    await delay(200);
    const force = new URL(request.url).searchParams.get('force') === 'true';
    if (force) {
      pushSystemAuditEvent({
        actor_type: 'system',
        actor_id: 'system',
        action: 'codec_probe.retested',
        resource_type: 'codec_probe',
        resource_id: 'ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers',
        before_json: null,
        after_json: null,
        metadata_json: { ffmpeg_version: 'ffmpeg version 6.1.1', result_count: 5 },
        request_id: 'req-codec-retest-mock',
      });
    }
    return HttpResponse.json({
      ffmpeg_available: true,
      results: [
        {
          encoder: 'libx264',
          family: 'software',
          codec: 'h264',
          compiled: true,
          tested_ok: true,
          detail: '',
        },
        {
          encoder: 'libx265',
          family: 'software',
          codec: 'h265',
          compiled: true,
          tested_ok: true,
          detail: '',
        },
        {
          encoder: 'h264_amf',
          family: 'amf',
          codec: 'h264',
          compiled: true,
          tested_ok: true,
          detail: '',
        },
        {
          encoder: 'hevc_amf',
          family: 'amf',
          codec: 'h265',
          compiled: true,
          tested_ok: true,
          detail: '',
        },
        {
          encoder: 'av1_amf',
          family: 'amf',
          codec: 'av1',
          compiled: true,
          tested_ok: false,
          detail: '[av1_amf @ 0x55] AMF 不支持 AV1 编码',
        },
      ],
      from_cache: !force,
      ffmpeg_version: 'ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers',
      tested_at: '2026-06-23T10:00:00Z',
    });
  }),

  // 环境变量查看（FR-56）：只读，敏感项已脱敏（绝不含明文）
  http.get('*/api/system/env', async () => {
    await delay(80);
    return HttpResponse.json({
      env: [
        {
          key: 'JIANVIDEO_FFMPEG_PATH',
          description: 'ffmpeg 可执行文件路径，未设置时回退同目录捆绑版或 PATH',
          sensitive: false,
          set: true,
          value: '/opt/jianvideo/ffmpeg',
        },
        {
          key: 'JIANVIDEO_DEBUG',
          description: '设为 1/true 时启用 gin debug 模式（输出调试日志）',
          sensitive: false,
          set: false,
          value: '',
        },
        {
          key: 'JWT_SECRET',
          description: 'JWT 签名密钥，未设置时启动随机生成（重启后需重新登录）',
          sensitive: true,
          set: true,
          value: '****（已设置）',
        },
        {
          key: 'SMB_MASTER_PASSWORD',
          description: 'SMB 凭据加解密主密码，未设置则 SMB 凭据功能不可用',
          sensitive: true,
          set: false,
          value: '（未设置）',
        },
      ],
    });
  }),

  // FFmpeg 路径检测（FR-56）：含 ffmpeg 字样或空路径视为可用
  http.post('*/api/system/ffmpeg/detect', async ({ request }) => {
    await delay(120);
    const body = (await request.json()) as { path?: string };
    const path = body.path || '';
    const available = !path || path.toLowerCase().includes('ffmpeg');
    return HttpResponse.json({
      ffmpeg_available: available,
      ffmpeg_version: available
        ? 'ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers'
        : '',
    });
  }),

  // 代理连通性测试（FR-89）：含 bad 字样视为不可达，其余（含空=直连）视为可达
  http.post('*/api/system/proxy/test', async ({ request }) => {
    await delay(120);
    const body = (await request.json()) as { proxy?: string; target?: string };
    const proxy = body.proxy || '';
    const reachable = !proxy || !proxy.toLowerCase().includes('bad');
    return HttpResponse.json({
      reachable,
      detail: reachable ? 'HTTP 200' : 'dial tcp: connection refused',
      latency_ms: reachable ? 123 : 0,
      target: body.target || 'https://api.github.com',
    });
  }),

  http.get('*/api/system/tools', async () => {
    await delay(80);
    return HttpResponse.json({ items: toolStatuses });
  }),

  http.get('*/api/system/tools/sources', async () => {
    await delay(80);
    return HttpResponse.json({ sources: toolSources });
  }),

  http.post('*/api/system/tools/download', async ({ request }) => {
    await delay(120);
    const body = (await request.json()) as { tool?: string };
    const tool = body.tool || 'ffmpeg';
    const now = new Date().toISOString();
    const taskID = `tool-download-${Date.now()}`;
    addUnifiedTask({
      id: taskID,
      scope: 'system',
      space_id: null,
      type: 'tool.download',
      status: 'running',
      priority: 5,
      attempts: 1,
      max_attempts: 1,
      progress: 0.45,
      checkpoint: '下载中',
      resource_type: 'tool',
      resource_id: tool,
      error: null,
      created_at: now,
      updated_at: now,
      started_at: now,
    });
    return HttpResponse.json({ status: 'queued', task_id: taskID }, { status: 202 });
  }),

  // 自更新（FR-46）
  http.get('*/api/system/update/check', async ({ request }) => {
    await delay(150);
    const channel = new URL(request.url).searchParams.get('channel') || 'stable';
    const prerelease = channel === 'prerelease';
    return HttpResponse.json({
      current: '0.3.0',
      latest: prerelease ? 'v0.6.3-dev.abc1234' : 'v0.6.3',
      has_update: true,
      tag: prerelease ? 'v0.6.3-dev.abc1234' : 'v0.6.3',
      prerelease,
      channel,
      notes: '示例发布说明',
      asset_name: 'jianvideo-linux-amd64',
      rollback_available: false,
    });
  }),
  http.post('*/api/system/update/apply', async () => {
    await delay(100);
    return HttpResponse.json({ status: 'updating', message: '更新已应用，服务即将重启' });
  }),
  http.post('*/api/system/update/rollback', async () => {
    await delay(100);
    return HttpResponse.json({ status: 'rolling_back', message: '已回滚到上一版本，服务即将重启' });
  }),
  // 自更新下载进度（FR-90）
  http.get('*/api/system/update/progress', async () => {
    await delay(50);
    return HttpResponse.json({
      state: 'downloading',
      downloaded: 6 * 1024 * 1024,
      total: 12 * 1024 * 1024,
      percent: 50,
    });
  }),

  // ─── 审计事件（FR2-040）────────────────────────────────

  http.get('*/api/audit/events', async ({ request }) => {
    await delay(80);
    const url = new URL(request.url);
    const limit = Number(url.searchParams.get('limit') || '20');
    const cursor = Number(url.searchParams.get('cursor') || '0');
    const action = url.searchParams.get('action') || '';
    const resourceType = url.searchParams.get('resource_type') || '';
    const resourceID = url.searchParams.get('resource_id') || '';
    const spaceID = url.searchParams.get('space_id') || '';
    const scope = url.searchParams.get('scope') === 'system' ? 'system' : 'space';
    const from = url.searchParams.get('from') || '';
    const to = url.searchParams.get('to') || '';

    let items = auditEvents.filter((event) => {
      if (event.scope !== scope) return false;
      if (action && event.action !== action) return false;
      if (resourceType && event.resource_type !== resourceType) return false;
      if (resourceID && event.resource_id !== resourceID) return false;
      if (spaceID && event.space_id !== spaceID) return false;
      if (from && event.created_at < from) return false;
      if (to && event.created_at > to) return false;
      return true;
    });
    items = items.sort((a, b) => b.created_at.localeCompare(a.created_at) || b.id - a.id);

    const pageItems = items.slice(cursor, cursor + limit);
    const nextCursor = cursor + limit < items.length ? String(cursor + limit) : null;
    return HttpResponse.json({ items: pageItems, next_cursor: nextCursor });
  }),

  // ─── 运行期设置 ────────────────────────────────────────

  http.get('*/api/settings/storage', async () => {
    await delay(80);
    return HttpResponse.json({
      space: { id: 'space-default', name: '默认 Space', owner_user_id: 1 },
      data_dir: 'data',
      database_path: 'data/jianvideo.db',
      library_count: paths.length,
    });
  }),

  http.get('*/api/settings', async () => {
    await delay(100);
    return HttpResponse.json({ settings: { ...settingsStore } });
  }),

  http.get('*/api/settings/definitions', async () => {
    await delay(80);
    return HttpResponse.json({ definitions: settingDefinitions });
  }),

  http.put('*/api/settings', async ({ request }) => {
    await delay(100);
    const body = (await request.json()) as { settings: Record<string, string> };
    if (!body.settings || Object.keys(body.settings).length === 0) {
      return HttpResponse.json(
        { code: 'EMPTY_SETTINGS', message: 'settings 不能为空' },
        { status: 400 },
      );
    }
    const badKey = Object.keys(body.settings).find((key) => !writableSettingKeys.has(key));
    if (badKey) {
      return HttpResponse.json(
        { code: 'INVALID_SETTING', message: `${badKey}: 未知设置项` },
        { status: 400 },
      );
    }
    const before = Object.fromEntries(
      Object.keys(body.settings).map((key) => [key, settingsStore[key] ?? '']),
    );
    Object.assign(settingsStore, body.settings);
    pushSystemAuditEvent({
      actor_type: 'system',
      actor_id: 'system',
      action: 'settings.updated',
      resource_type: 'settings',
      resource_id: 'settings',
      before_json: before,
      after_json: { ...body.settings },
      metadata_json: { summary: '更新运行期设置' },
      request_id: 'req-settings-mock',
    });
    return HttpResponse.json({ settings: { ...settingsStore } });
  }),

  // ─── 通用任务中心（FR2-037）─────────────────────────────

  http.get('*/api/tasks', async ({ request }) => {
    await delay(80);
    const url = new URL(request.url);
    const page = Number(url.searchParams.get('page') || '1');
    const pageSize = Number(url.searchParams.get('page_size') || '20');
    visibleUnifiedTasks(request).forEach(pollCacheInventoryTask);
    const items = visibleUnifiedTasks(request);
    const start = (page - 1) * pageSize;
    return HttpResponse.json({
      items: items.slice(start, start + pageSize),
      page,
      page_size: pageSize,
      total: items.length,
    });
  }),

  http.get('*/api/tasks/stats', async ({ request }) => {
    await delay(80);
    const items = visibleUnifiedTasks(request);
    return HttpResponse.json({
      total: items.length,
      by_status: countTaskStatuses(items),
      by_type: countTasksBy(items, (task) => task.type),
    });
  }),

  http.get('*/api/tasks/:id', async ({ request, params }) => {
    await delay(80);
    const id = String(params.id);
    const item = visibleUnifiedTasks(request).find((task) => task.id === id);
    if (!item) {
      return HttpResponse.json({ code: 'TASK_NOT_FOUND', message: '任务不存在' }, { status: 404 });
    }
    pollCacheInventoryTask(item);
    return HttpResponse.json(item);
  }),

  http.get('*/api/storage/cache/summary', async () => {
    await delay(80);
    const rows = Object.values(cacheRows);
    return HttpResponse.json({
      total_size_bytes: rows.reduce((sum, row) => sum + row.size_bytes, 0),
      total_file_count: rows.reduce((sum, row) => sum + row.file_count, 0),
      total_assets: rows.reduce((sum, row) => sum + row.asset_count, 0),
      by_kind: Object.fromEntries(rows.map((row) => [row.kind, { ...row, rebuildable: true }])),
    });
  }),

  http.get('*/api/storage/cache/assets', async () => {
    await delay(80);
    return HttpResponse.json({ items: [], page: 1, page_size: 20, total: 0 });
  }),

  http.post('*/api/storage/cache/inventory', async () => {
    await delay(120);
    const taskID = nextCacheTaskID++;
    const id = String(taskID);
    const now = new Date().toISOString();
    cacheInventoryPolls.set(id, 0);
    addUnifiedTask({
      id,
      scope: 'space',
      space_id: 'space-default',
      type: 'cache.inventory',
      status: 'running',
      priority: 0,
      attempts: 0,
      max_attempts: 3,
      progress: 0,
      resource_type: 'cache',
      resource_id: 'inventory',
      error: null,
      created_at: now,
      updated_at: now,
      started_at: now,
    });
    return HttpResponse.json({ task_id: taskID }, { status: 202 });
  }),

  http.post('*/api/storage/cache/clean', async ({ request }) => {
    await delay(120);
    const body = (await request.json()) as { dry_run?: boolean; kinds?: MockCacheKind[] };
    const kinds = body.kinds?.length ? body.kinds : [...cacheKinds];
    const selected = kinds.map((kind) => cacheRows[kind]).filter(Boolean);
    const taskID = body.dry_run ? undefined : nextCacheTaskID++;
    const result = {
      dry_run: Boolean(body.dry_run),
      task_id: taskID,
      candidate_count: selected.reduce((sum, row) => sum + row.asset_count, 0),
      total_size_bytes: selected.reduce((sum, row) => sum + row.size_bytes, 0),
      total_file_count: selected.reduce((sum, row) => sum + row.file_count, 0),
      deleted_count: 0,
      deleted_size_bytes: 0,
      failed_count: 0,
    };
    if (!body.dry_run) {
      for (const kind of kinds) {
        cacheRows[kind] = { ...cacheRows[kind], size_bytes: 0, file_count: 0, asset_count: 0 };
      }
      result.deleted_count = result.candidate_count;
      result.deleted_size_bytes = result.total_size_bytes;
      const now = new Date().toISOString();
      addUnifiedTask({
        id: String(taskID),
        scope: 'space',
        space_id: 'space-default',
        type: 'cache.clean',
        status: 'succeeded',
        priority: 0,
        attempts: 0,
        max_attempts: 1,
        progress: 1,
        resource_type: 'cache',
        resource_id: 'clean',
        error: null,
        created_at: now,
        updated_at: now,
        finished_at: now,
      });
    }
    return HttpResponse.json(result, { status: body.dry_run ? 200 : 202 });
  }),

  http.post('*/api/tasks/:id/cancel', async ({ request, params }) => {
    await delay(80);
    const id = String(params.id);
    const item = visibleUnifiedTasks(request).find((task) => task.id === id);
    if (!item) {
      return HttpResponse.json({ code: 'TASK_NOT_FOUND', message: '任务不存在' }, { status: 404 });
    }
    if (item.status !== 'pending' && item.status !== 'running') {
      return HttpResponse.json(
        { code: 'TASK_OPERATION_FAILED', message: '仅 pending 或 running 任务可取消' },
        { status: 400 },
      );
    }
    item.status = 'canceled';
    item.updated_at = new Date().toISOString();
    item.finished_at = item.updated_at;
    return HttpResponse.json(item);
  }),

  http.post('*/api/tasks/:id/retry', async ({ request, params }) => {
    await delay(80);
    const id = String(params.id);
    const item = visibleUnifiedTasks(request).find((task) => task.id === id);
    if (!item) {
      return HttpResponse.json({ code: 'TASK_NOT_FOUND', message: '任务不存在' }, { status: 404 });
    }
    if (item.status !== 'failed' && item.status !== 'canceled') {
      return HttpResponse.json(
        { code: 'TASK_OPERATION_FAILED', message: '仅 failed 或 canceled 任务可重试' },
        { status: 400 },
      );
    }
    item.status = 'pending';
    item.progress = 0;
    item.error = null;
    item.attempts = 0;
    item.updated_at = new Date().toISOString();
    item.started_at = null;
    item.finished_at = null;
    return HttpResponse.json(item);
  }),

  // ─── 扫描 ──────────────────────────────────────────────

  http.post('*/api/library/scan/:id', async ({ params }) => {
    await delay(500);
    const id = Number(params.id);
    const libraryPath = paths.find((p) => p.id === id)?.path || 'D:\\Videos';
    // 模拟扫描：随机添加 1-3 个新文件
    const count = Math.floor(Math.random() * 3) + 1;
    const formats = ['mp4', 'mkv', 'avi', 'mov'];
    for (let i = 0; i < count; i++) {
      const fileId = nextMediaId++;
      const format = formats[i % formats.length];
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
      };
      mediaFiles.push(newFile);
    }
    // 扫描任务队列（FR-29）：入队一条已完成任务，供页眉任务展示
    const now = new Date().toISOString();
    scanTasks.unshift({
      id: nextScanTaskId++,
      library_id: id,
      scan_type: 'full',
      status: 'completed',
      scanned_files: count,
      total_files: count,
      error: '',
      created_at: now,
      started_at: now,
      completed_at: now,
    });
    addUnifiedTask({
      id: `scan-${nextScanTaskId - 1}`,
      scope: 'space',
      space_id: 'space-default',
      type: 'library.scan',
      status: 'succeeded',
      priority: 0,
      attempts: 0,
      max_attempts: 1,
      progress: 1,
      resource_type: 'library',
      resource_id: String(id),
      error: null,
      created_at: now,
      updated_at: now,
      started_at: now,
      finished_at: now,
    });
    return HttpResponse.json({ status: 'queued', task_id: nextScanTaskId - 1 });
  }),

  // 扫描任务列表（FR-29）
  http.get('*/api/library/scan/tasks', async () => {
    await delay(50);
    const current = scanTasks.find((t) => t.status === 'running') ?? null;
    return HttpResponse.json({ tasks: [...scanTasks], current });
  }),

  // ─── 相册（FR-40）──────────────────────────────────────

  http.get('*/api/albums', async () => {
    await delay(120);
    const items = albums.map((a) => ({
      ...a,
      item_count: albumItems.filter((it) => it.album_id === a.id).length,
    }));
    return HttpResponse.json({ items });
  }),

  http.post('*/api/albums', async ({ request }) => {
    await delay(150);
    const body = (await request.json()) as { name: string; description?: string };
    const name = (body.name || '').trim();
    if (!name) {
      return HttpResponse.json(
        { code: 'INVALID_INPUT', message: '相册名称不能为空' },
        { status: 400 },
      );
    }
    const now = new Date().toISOString();
    const album: Album = {
      id: nextAlbumId++,
      name,
      description: (body.description || '').trim(),
      cover_media_id: 0,
      created_at: now,
      updated_at: now,
    };
    albums.unshift(album);
    return HttpResponse.json(album, { status: 201 });
  }),

  http.delete('*/api/albums/:id', async ({ params }) => {
    await delay(120);
    const id = Number(params.id);
    if (!albums.some((a) => a.id === id)) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '相册不存在' }, { status: 404 });
    }
    albums = albums.filter((a) => a.id !== id);
    albumItems = albumItems.filter((it) => it.album_id !== id);
    return new HttpResponse(null, { status: 204 });
  }),

  http.get('*/api/albums/:id/items', async ({ params }) => {
    await delay(120);
    const id = Number(params.id);
    const items = albumItems
      .filter((it) => it.album_id === id)
      .map((it) => mediaFiles.find((m) => m.id === it.media_id))
      .filter((m): m is MediaFile => m !== undefined);
    return HttpResponse.json({ items });
  }),

  http.post('*/api/albums/:id/items', async ({ params, request }) => {
    await delay(120);
    const id = Number(params.id);
    const body = (await request.json()) as { media_id: number };
    if (!albums.some((a) => a.id === id) || !mediaFiles.some((m) => m.id === body.media_id)) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '相册或媒体不存在' }, { status: 404 });
    }
    if (!albumItems.some((it) => it.album_id === id && it.media_id === body.media_id)) {
      albumItems.push({ album_id: id, media_id: body.media_id });
    }
    return new HttpResponse(null, { status: 204 });
  }),

  http.delete('*/api/albums/:id/items/:mediaId', async ({ params }) => {
    await delay(120);
    const id = Number(params.id);
    const mediaId = Number(params.mediaId);
    albumItems = albumItems.filter((it) => !(it.album_id === id && it.media_id === mediaId));
    return new HttpResponse(null, { status: 204 });
  }),

  // ─── 那年今日（FR-72）──────────────────────────────────

  // 往年同一天的媒体回忆：mock 不复刻真实拍摄日匹配，默认返回空列表（与「最近查看」一致，
  // 避免污染依赖空回忆区的页面用例）；需要回忆数据的用例用 server.use 覆盖。
  http.get('*/api/library/on-this-day', async () => {
    await delay(100);
    return HttpResponse.json({ items: [] });
  }),

  // ─── 感知哈希去重（FR-70）──────────────────────────────

  // 触发去重扫描：mock 直接返回一个非零的本次新算条数
  http.post('*/api/library/duplicates/scan', async () => {
    await delay(300);
    return HttpResponse.json({ computed: 2 });
  }),

  // 查询重复组：mock 默认无重复组（需要重复组的用例用 server.use 覆盖）
  http.get('*/api/library/duplicates', async () => {
    await delay(150);
    return HttpResponse.json({ groups: [] });
  }),

  // 触发内容哈希回填：mock 只入通用任务，不在请求线程内计算。
  http.post('*/api/library/file-hashes/backfill', async ({ request }) => {
    await delay(120);
    const spaceID = currentSpaceID(request);
    const taskID = `file-hash-${nextFileHashTaskId++}`;
    addUnifiedTask({
      id: taskID,
      scope: 'space',
      space_id: spaceID,
      type: 'library.file_hash_backfill',
      status: 'pending',
      priority: 0,
      attempts: 0,
      max_attempts: 3,
      progress: 0,
      resource_type: 'library',
      resource_id: spaceID,
      error: null,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    });
    return HttpResponse.json({ status: 'queued', task_id: taskID }, { status: 202 });
  }),

  // 查询精确重复组：mock 默认无重复组（需要重复组的用例用 server.use 覆盖）
  http.get('*/api/library/duplicates/exact', async () => {
    await delay(150);
    return HttpResponse.json({ groups: [] });
  }),

  // ─── 观看统计 / 概览 / 趋势 ─────────────────────────────

  // 观看统计（FR-75）：与 api 层 mock 形状一致，便于离线 demo 展示各维度
  http.get('*/api/library/stats', async () => {
    await delay(120);
    return HttpResponse.json({
      total: 42,
      watched: 18,
      unwatched: 24,
      recent_timeline: [
        { date: '2026-06-24', count: 5 },
        { date: '2026-06-23', count: 3 },
        { date: '2026-06-22', count: 8 },
        { date: '2026-06-20', count: 2 },
      ],
      position_heatmap: [3, 1, 0, 2, 1, 4, 0, 1, 2, 6],
      by_library: [
        { library_id: 1, label: '电影', watched: 12 },
        { library_id: 2, label: '剧集', watched: 6 },
      ],
      by_format: [
        { format: 'mp4', watched: 11 },
        { format: 'mkv', watched: 7 },
      ],
      top_viewed: [],
    });
  }),

  // 媒体库概览（FR-117）：未软删媒体的数量与体量聚合
  http.get('*/api/library/summary', async () => {
    await delay(120);
    return HttpResponse.json({
      total: 12480,
      video_count: 3210,
      image_count: 9270,
      total_size: 1979900000000,
      total_duration: 2311200,
      library_count: 3,
      by_library: [
        {
          library_id: 1,
          label: '电影',
          media_count: 3460,
          video_count: 3460,
          image_count: 0,
          total_size: 580000000000,
          total_duration: 1980000,
        },
        {
          library_id: 2,
          label: '照片',
          media_count: 8200,
          video_count: 120,
          image_count: 8080,
          total_size: 320000000000,
          total_duration: 28800,
        },
        {
          library_id: 3,
          label: '剧集',
          media_count: 820,
          video_count: 820,
          image_count: 0,
          total_size: 1079900000000,
          total_duration: 302400,
        },
      ],
    });
  }),

  // 媒体增长趋势（FR-118）：按天的新增数量 / 体量 / 时长
  http.get('*/api/library/trends', async () => {
    await delay(120);
    return HttpResponse.json({
      media_added: [
        { date: '2026-05-01', count: 10, size: 1000000000, duration: 3000 },
        { date: '2026-05-03', count: 5, size: 500000000, duration: 1500 },
        { date: '2026-05-08', count: 8, size: 820000000, duration: 2400 },
        { date: '2026-05-15', count: 3, size: 300000000, duration: 900 },
        { date: '2026-06-02', count: 12, size: 1500000000, duration: 4200 },
        { date: '2026-06-20', count: 6, size: 640000000, duration: 1800 },
      ],
    });
  }),

  // ─── 媒体健康巡检（FR-73）──────────────────────────────

  // 触发巡检：mock 巡检为空操作，直接回 scanning
  http.post('*/api/library/health/scan', async () => {
    await delay(200);
    return HttpResponse.json({ status: 'scanning' });
  }),

  // 巡检进度：mock 直接 completed、无问题
  http.get('*/api/library/health/status', async () => {
    await delay(80);
    return HttpResponse.json({
      status: 'completed',
      total: 0,
      checked: 0,
      issue_count: 0,
      error: '',
      started_at: '',
      completed_at: new Date().toISOString(),
    });
  }),

  // 巡检问题列表：mock 默认无问题
  http.get('*/api/library/health/issues', async () => {
    await delay(80);
    return HttpResponse.json({ items: [] });
  }),

  // ─── 系统监控指标（FR-119）──────────────────────────────

  // 系统指标时序：mock 给一组多点示例 + current 快照
  http.get('*/api/system/metrics', async ({ request }) => {
    await delay(120);
    const range = new URL(request.url).searchParams.get('range') || '1h';
    const current = {
      t: '2026-06-27T14:00:00Z',
      cpu_percent: 47.2,
      mem_used_bytes: 202000000,
      mem_sys_bytes: 540000000,
      disk_used_bytes: 1980900000000,
      disk_total_bytes: 2600000000000,
      transcode_active: 2,
      goroutines: 122,
    };
    return HttpResponse.json({
      range,
      points: [
        {
          t: '2026-06-27T10:00:00Z',
          cpu_percent: 32.5,
          mem_used_bytes: 180000000,
          mem_sys_bytes: 520000000,
          disk_used_bytes: 1979900000000,
          disk_total_bytes: 2600000000000,
          transcode_active: 1,
          goroutines: 110,
        },
        {
          t: '2026-06-27T12:00:00Z',
          cpu_percent: 28.3,
          mem_used_bytes: 188000000,
          mem_sys_bytes: 530000000,
          disk_used_bytes: 1980300000000,
          disk_total_bytes: 2600000000000,
          transcode_active: 0,
          goroutines: 112,
        },
        current,
      ],
      current,
    });
  }),

  // ─── 公开分享（FR-43/FR-78）─────────────────────────────

  // 创建分享：返回新建的 Share 对象（与后端一致，不包裹）
  http.post('*/api/shares', async ({ request }) => {
    await delay(120);
    const body = (await request.json()) as {
      resource_type: ShareResourceType;
      resource_id: number;
      expires_in_hours?: number;
      password?: string;
      max_uses?: number;
    };
    const now = new Date();
    const expiresInHours = body.expires_in_hours ?? 0;
    const share: Share = {
      token: `mock-token-${nextShareTokenSeq++}`,
      resource_type: body.resource_type,
      resource_id: body.resource_id,
      expires_at:
        expiresInHours > 0
          ? new Date(now.getTime() + expiresInHours * 3600_000).toISOString()
          : null,
      max_uses: body.max_uses ?? 0,
      used_count: 0,
      created_at: now.toISOString(),
    };
    shares.unshift(share);
    return HttpResponse.json(share, { status: 201 });
  }),

  // 分享列表：以 { shares } 包裹返回（与后端一致）
  http.get('*/api/shares', async () => {
    await delay(120);
    return HttpResponse.json({ shares: [...shares] });
  }),

  // 撤销分享：删除对应 token
  http.delete('*/api/shares/:token', async ({ params }) => {
    await delay(120);
    const token = String(params.token);
    shares = shares.filter((s) => s.token !== token);
    return new HttpResponse(null, { status: 204 });
  }),

  // 分享元信息（FR-43/FR-78）：媒体分享带 media，相册分享带空 items；不存在返回 404
  http.get('*/api/share/:token', async ({ params }) => {
    await delay(120);
    const token = String(params.token);
    const share = shares.find((s) => s.token === token);
    if (!share) {
      return HttpResponse.json(
        { code: 'NOT_FOUND', message: '分享不存在或已过期' },
        { status: 404 },
      );
    }
    if (share.resource_type === 'media') {
      const media = mediaFiles.find((m) => m.id === share.resource_id);
      return HttpResponse.json({
        resource_type: 'media',
        expires_at: share.expires_at,
        media,
      });
    }
    return HttpResponse.json({
      resource_type: 'album',
      expires_at: share.expires_at,
      items: [],
    });
  }),

  // ─── 转码预设与预生成任务（FR-77）───────────────────────

  http.get('*/api/transcode/presets', async () => {
    await delay(80);
    return HttpResponse.json({ items: [...transcodePresets] });
  }),

  http.post('*/api/transcode/presets', async ({ request }) => {
    await delay(80);
    const body = (await request.json()) as {
      name: string;
      codec: TranscodeCodec;
      width: number;
      height: number;
    };
    const name = (body.name || '').trim();
    if (!name) {
      return HttpResponse.json(
        { code: 'INVALID_INPUT', message: '预设名不能为空' },
        { status: 400 },
      );
    }
    if (!KNOWN_TRANSCODE_CODECS.includes(body.codec)) {
      return HttpResponse.json(
        { code: 'INVALID_CODEC', message: '不支持的目标编码' },
        { status: 400 },
      );
    }
    const now = new Date().toISOString();
    const preset: TranscodePreset = {
      id: nextPresetId++,
      name,
      codec: body.codec,
      width: body.width,
      height: body.height,
      created_at: now,
      updated_at: now,
    };
    transcodePresets.unshift(preset);
    return HttpResponse.json(preset, { status: 201 });
  }),

  http.put('*/api/transcode/presets/:id', async ({ request, params }) => {
    await delay(80);
    const id = Number(params.id);
    const preset = transcodePresets.find((p) => p.id === id);
    if (!preset) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '预设不存在' }, { status: 404 });
    }
    const body = (await request.json()) as {
      name: string;
      codec: TranscodeCodec;
      width: number;
      height: number;
    };
    preset.name = (body.name || '').trim();
    preset.codec = body.codec;
    preset.width = body.width;
    preset.height = body.height;
    preset.updated_at = new Date().toISOString();
    return HttpResponse.json({ ...preset });
  }),

  http.delete('*/api/transcode/presets/:id', async ({ params }) => {
    await delay(80);
    const id = Number(params.id);
    transcodePresets = transcodePresets.filter((p) => p.id !== id);
    return new HttpResponse(null, { status: 204 });
  }),

  http.post('*/api/transcode/tasks', async ({ request }) => {
    await delay(80);
    const body = (await request.json()) as { media_id: number; preset_id: number };
    const preset = transcodePresets.find((p) => p.id === body.preset_id);
    if (!preset) {
      return HttpResponse.json({ code: 'NOT_FOUND', message: '预设不存在' }, { status: 404 });
    }
    const now = new Date().toISOString();
    const task: TranscodeTask = {
      id: nextTranscodeTaskId++,
      media_id: body.media_id,
      preset_id: body.preset_id,
      codec: preset.codec,
      width: preset.width,
      height: preset.height,
      status: 'completed',
      error: '',
      created_at: now,
      started_at: now,
      completed_at: now,
    };
    transcodeTasks.unshift(task);
    addUnifiedTask({
      id: `transcode-${task.id}`,
      scope: 'space',
      space_id: 'space-default',
      type: 'transcode.hls',
      status: 'succeeded',
      priority: 0,
      attempts: 0,
      max_attempts: 1,
      progress: 1,
      resource_type: 'media',
      resource_id: String(body.media_id),
      error: null,
      created_at: now,
      updated_at: now,
      started_at: now,
      finished_at: now,
    });
    return HttpResponse.json({ status: 'queued', task_id: task.id }, { status: 202 });
  }),

  http.get('*/api/transcode/tasks', async ({ request }) => {
    await delay(80);
    const status = new URL(request.url).searchParams.get('status') || '';
    const tasks = status ? transcodeTasks.filter((t) => t.status === status) : [...transcodeTasks];
    return HttpResponse.json({ tasks });
  }),

  // ─── 健康探活 ───────────────────────────────────────────

  // 服务在线探测（自更新/回滚重启后轮询恢复用）
  http.get('*/health', async () => {
    await delay(20);
    return HttpResponse.json({ status: 'ok' });
  }),
];
