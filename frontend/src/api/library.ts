import type {
  LibraryPath,
  LibraryKind,
  LibraryKindInfo,
  MediaFile,
  MediaListResponse,
  ScanResponse,
  ScanMode,
  BrowseResponse,
  MediaExtension,
  MediaExtensionType,
  MediaTypesResponse,
  MediaTypeRule,
  CreateMediaTypeRuleInput,
  UpdateMediaTypeRuleInput,
  ScanStatus,
  ScanTask,
  ScanTasksResponse,
  Tag,
  RecycleCleanupResult,
  DuplicateGroup,
  ExactDuplicateGroup,
  FileHashBackfillResponse,
  UploadNamingRule,
  UploadResponse,
  MediaInference,
  MediaInferenceInput,
  MediaMetadata,
  MediaCover,
  MediaCoversResponse,
} from '@/types';

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true';

// ─── Mock 数据（统一来源） ────────────────────────────

import { mockPaths, mockMediaFiles } from '@/mocks/data';

let nextMockId = 100;
let nextMockExtensionId = 1;
const mockExtensions: MediaExtension[] = [];
const mockBuiltinRuleOverrides: MediaTypeRule[] = [];

export const defaultLibraryKinds: LibraryKindInfo[] = [
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
];

const defaultMediaTypes: MediaTypesResponse['types'] = [
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

function builtinMockMediaTypeRules(libraryID = 0): MediaTypeRule[] {
  return defaultMediaTypes.flatMap((type) =>
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

function mockMediaRuleKey(rule: Pick<MediaTypeRule, 'library_id' | 'type' | 'extension'>): string {
  return `${rule.library_id ?? 0}:${rule.type}:${rule.extension}`;
}

function effectiveBuiltinMockMediaTypeRules(libraryID = 0): MediaTypeRule[] {
  const overrides = new Map(mockBuiltinRuleOverrides.map((rule) => [mockMediaRuleKey(rule), rule]));
  return builtinMockMediaTypeRules(libraryID).map((rule) => {
    return overrides.get(mockMediaRuleKey(rule)) ?? rule;
  });
}

// 标签 mock 状态（FR-41）：标签表 + 媒体-标签映射
let nextMockTagId = 1;
const mockTags: Tag[] = [];
const mockTagMappings: { tag_id: number; media_id: number }[] = [];

// 软删除/回收站 mock 状态（FR-25）：被软删的媒体 ID 集合
const mockDeletedIds = new Set<number>();
const mockInferences = new Map<number, MediaInference>();

// 扫描任务队列 mock 状态（FR-29）
let nextMockTaskId = 1;
const mockScanTasks: ScanTask[] = [];

function mockDelay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

// ─── 真实 API 实现 ──────────────────────────────────

import client from './client';
import type { AxiosError } from 'axios';

function getApiErrorMessage(err: unknown, fallback: string): string {
  const error = err as AxiosError<{ message?: string }>;
  return error.response?.data?.message || (err instanceof Error ? err.message : fallback);
}

async function realGetLibraryPaths(): Promise<LibraryPath[]> {
  const res = await client.get<{ items: LibraryPath[] }>('/api/library/paths');
  return res.data.items;
}

async function realGetLibraryKinds(): Promise<LibraryKindInfo[]> {
  const res = await client.get<{ items: LibraryKindInfo[] }>('/api/library/kinds');
  return res.data.items;
}

async function realCreateLibraryPath(
  path: string,
  type = 'local',
  label = '',
  libraryKind: LibraryKind = 'mixed',
): Promise<LibraryPath> {
  try {
    const res = await client.post<LibraryPath>('/api/library/paths', {
      path,
      type,
      label,
      library_kind: libraryKind,
    });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '无法添加目录，请检查路径是否正确'), { cause: err });
  }
}

async function realUpdateLibraryPath(
  id: number,
  input: Partial<Pick<LibraryPath, 'label' | 'enabled' | 'library_kind'>>,
): Promise<LibraryPath> {
  const res = await client.put<LibraryPath>(`/api/library/paths/${id}`, input);
  return res.data;
}

async function realDeleteLibraryPath(id: number): Promise<void> {
  await client.delete(`/api/library/paths/${id}`);
}

// 媒体列表查询参数（FR-41 收藏/标签 + FR-35/36 结构化筛选与表达式搜索）
export interface MediaListParams {
  library_id?: number;
  sort?: string;
  page?: number;
  page_size?: number;
  search?: string;
  favorite?: boolean;
  tag_id?: number;
  // FR-35/36 结构化筛选
  type?: 'image' | 'video';
  size_min?: number;
  size_max?: number;
  time_from?: string;
  time_to?: string;
  path?: string;
  // FR-39 照片地图：仅带 GPS 的媒体
  has_gps?: boolean;
  // FR2-031 本地影视信息筛选
  inference?: 'inferred' | 'auto' | 'manual' | 'missing';
}

async function realGetMediaFiles(params: MediaListParams = {}): Promise<MediaListResponse> {
  const res = await client.get<MediaListResponse>('/api/library/media', { params });
  return res.data;
}

// ─── 收藏与标签（FR-41）──────────────────────────────

async function realSetMediaFavorite(id: number, favorite: boolean): Promise<MediaFile> {
  const res = await client.put<MediaFile>(`/api/library/media/${id}/favorite`, { favorite });
  return res.data;
}

async function realGetTags(): Promise<Tag[]> {
  const res = await client.get<{ items: Tag[] }>('/api/library/tags');
  return res.data.items;
}

async function realCreateTag(name: string): Promise<Tag> {
  try {
    const res = await client.post<Tag>('/api/library/tags', { name });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '创建标签失败'), { cause: err });
  }
}

async function realGetMediaTags(mediaID: number): Promise<Tag[]> {
  const res = await client.get<{ items: Tag[] }>(`/api/library/media/${mediaID}/tags`);
  return res.data.items;
}

async function realAddMediaTag(
  mediaID: number,
  tag: { tag_id?: number; name?: string },
): Promise<Tag> {
  try {
    const res = await client.post<Tag>(`/api/library/media/${mediaID}/tags`, tag);
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '打标签失败'), { cause: err });
  }
}

async function realRemoveMediaTag(mediaID: number, tagID: number): Promise<void> {
  await client.delete(`/api/library/media/${mediaID}/tags/${tagID}`);
}

// ─── 续播与观看状态（FR-44）─────────────────────────

async function realUpdateWatchPosition(id: number, position: number): Promise<MediaFile> {
  const res = await client.put<MediaFile>(`/api/play/${id}/position`, { position });
  return res.data;
}

async function realMarkWatched(id: number): Promise<MediaFile> {
  const res = await client.put<MediaFile>(`/api/play/${id}/watched`);
  return res.data;
}

async function realGetContinueWatching(limit = 12): Promise<MediaFile[]> {
  const res = await client.get<{ items: MediaFile[] }>('/api/library/continue-watching', {
    params: { limit },
  });
  return res.data.items;
}

// 那年今日（FR-72）：拉取往年同一天拍摄的媒体回忆列表。
async function realGetOnThisDay(limit = 12): Promise<MediaFile[]> {
  const res = await client.get<{ items: MediaFile[] }>('/api/library/on-this-day', {
    params: { limit },
  });
  return res.data.items;
}

// ─── 最近查看（FR-120）────────────────────────────────

// 记录媒体查看：媒体在查看器/播放页被打开时调用，把 last_viewed_at 置为当前时间。
// 失败由调用方静默处理（不阻塞打开），此处不吞异常。
async function realSetMediaViewed(id: number, signal?: AbortSignal): Promise<void> {
  await client.put(`/api/library/media/${id}/viewed`, undefined, { signal });
}

// 最近查看列表：返回 last_viewed_at 非空、未软删的媒体，按 last_viewed_at 倒序。
async function realGetRecentlyViewed(limit = 12): Promise<MediaFile[]> {
  const res = await client.get<{ items: MediaFile[] }>('/api/library/recently-viewed', {
    params: { limit },
  });
  return res.data.items;
}

async function realGetMediaFile(id: number, signal?: AbortSignal): Promise<MediaFile> {
  const res = await client.get(`/api/library/media/${id}`, { signal });
  return res.data;
}

async function realGetMediaMetadata(id: number): Promise<MediaMetadata[]> {
  const res = await client.get<{ items: MediaMetadata[] }>(`/api/library/media/${id}/metadata`);
  return res.data.items;
}

async function realGetMediaCovers(id: number): Promise<MediaCoversResponse> {
  const res = await client.get<MediaCoversResponse>(`/api/library/media/${id}/covers`);
  return res.data;
}

async function realGenerateMediaCovers(id: number, refresh: boolean): Promise<{ status: string; task_id: number }> {
  const res = await client.post<{ status: string; task_id: number }>(
    `/api/library/media/${id}/covers/generate`,
    { refresh },
  );
  return res.data;
}

async function realSelectMediaCover(id: number, candidateID: number): Promise<MediaCover> {
  const res = await client.put<MediaCover>(`/api/library/media/${id}/cover`, {
    candidate_id: candidateID,
  });
  return res.data;
}

async function realGetMediaInference(id: number, signal?: AbortSignal): Promise<MediaInference | null> {
  const res = await client.get<{ inference: MediaInference | null }>(
    `/api/library/media/${id}/inference`,
    { signal },
  );
  return res.data.inference;
}

async function realUpdateMediaInference(
  id: number,
  input: MediaInferenceInput,
): Promise<MediaInference> {
  const res = await client.put<MediaInference>(`/api/library/media/${id}/inference`, input);
  return res.data;
}

export interface InferenceBackfillAccepted {
  status: 'pending' | 'running';
  task_id: number;
}

async function realBackfillMediaInferences(libraryID?: number): Promise<InferenceBackfillAccepted> {
  const res = await client.post<InferenceBackfillAccepted>(
    '/api/library/inference/backfill',
    libraryID ? { library_id: libraryID } : {},
  );
  return res.data;
}

async function realDeleteMediaFile(id: number): Promise<void> {
  await client.delete(`/api/library/media/${id}`);
}

// 批量软删（FR-69）：一次请求软删多个媒体，返回实际软删条数。
async function realBatchDeleteMediaFiles(ids: number[]): Promise<number> {
  const res = await client.post<{ deleted: number }>('/api/library/media/batch-delete', { ids });
  return res.data.deleted;
}

// ─── 感知哈希去重（FR-70）────────────────────────────

// 触发去重扫描：为缺 dHash 的媒体计算哈希，返回本次新算条数。
async function realScanDuplicates(): Promise<number> {
  const res = await client.post<{ computed: number }>('/api/library/duplicates/scan');
  return res.data.computed;
}

// 查询重复组：返回按汉明距离聚类、各 ≥2 项的近似重复组。
async function realGetDuplicateGroups(): Promise<DuplicateGroup[]> {
  const res = await client.get<{ groups: DuplicateGroup[] }>('/api/library/duplicates');
  return res.data.groups;
}

async function realBackfillFileHashes(): Promise<FileHashBackfillResponse> {
  const res = await client.post<FileHashBackfillResponse>('/api/library/file-hashes/backfill');
  return res.data;
}

async function realGetExactDuplicateGroups(): Promise<ExactDuplicateGroup[]> {
  const res = await client.get<{ groups: ExactDuplicateGroup[] }>('/api/library/duplicates/exact');
  return res.data.groups;
}

async function realRenameMediaFile(id: number, newName: string): Promise<MediaFile> {
  const res = await client.put<MediaFile>(`/api/library/media/${id}/rename`, { new_name: newName });
  return res.data;
}

async function realUpdateDisplayName(id: number, displayName: string): Promise<MediaFile> {
  try {
    const res = await client.put<MediaFile>(`/api/library/media/${id}/display-name`, {
      display_name: displayName,
    });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '修改显示名失败'), { cause: err });
  }
}

async function realUpdateMediaNotes(id: number, notes: string): Promise<MediaFile> {
  try {
    const res = await client.put<MediaFile>(`/api/library/media/${id}/notes`, { notes });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '保存备注失败'), { cause: err });
  }
}

// ─── 软删除与回收站（FR-25）──────────────────────────

async function realGetRecycleMediaFiles(): Promise<MediaFile[]> {
  const res = await client.get<{ items: MediaFile[] }>('/api/library/recycle');
  return res.data.items;
}

async function realRestoreMediaFile(id: number): Promise<void> {
  await client.post(`/api/library/media/${id}/restore`);
}

// 回收站清理（FR-26）：把全部软删项源文件移到盘符回收站目录并删记录，返回移动/失败统计。
async function realCleanupRecycle(): Promise<RecycleCleanupResult> {
  try {
    const res = await client.post<RecycleCleanupResult>('/api/library/recycle/cleanup');
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '清理回收站失败'), { cause: err });
  }
}

async function realScanLibrary(id: number, mode: ScanMode = 'incremental'): Promise<ScanResponse> {
  const res = await client.post<ScanResponse>(`/api/library/scan/${id}`, null, {
    params: { mode },
  });
  return res.data;
}

// ─── Web 上传入库（FR-149，见 ADR-0051）────────────────

/** 上传选项：临时指定落盘目录与命名规则（均可缺省，缺省走后端设置默认） */
export interface UploadMediaOptions {
  targetDir?: string;
  namingRule?: UploadNamingRule;
  // 上传进度回调（0~100），用于 UI 进度条
  onProgress?: (percent: number) => void;
}

async function realUploadMedia(file: File, opts: UploadMediaOptions = {}): Promise<UploadResponse> {
  try {
    const form = new FormData();
    form.append('file', file);
    if (opts.targetDir) form.append('target_dir', opts.targetDir);
    if (opts.namingRule) form.append('naming_rule', opts.namingRule);

    const res = await client.post<UploadResponse>('/api/library/upload', form, {
      // 上传大文件不设短超时（覆盖 client 默认 15s），由浏览器/服务端自然控制
      timeout: 0,
      onUploadProgress: (e) => {
        if (opts.onProgress && e.total) {
          opts.onProgress(Math.round((e.loaded / e.total) * 100));
        }
      },
    });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '上传失败'), { cause: err });
  }
}

// ─── 扫描任务队列（FR-29）──────────────────────────────

async function realGetScanTasks(): Promise<ScanTasksResponse> {
  // silent：页眉扫描任务指示器每 2s 轮询本端点，网络失败不弹全局 toast，
  // 避免持续失败时网络 toast 无上限堆积撑爆 DOM 致白屏（FR-115 后续修复·通知白屏）
  const res = await client.get<ScanTasksResponse>('/api/library/scan/tasks', { silent: true });
  return res.data;
}

async function realBrowseDirectory(parentPath: string, sort = 'name'): Promise<BrowseResponse> {
  try {
    // 真实路径树聚合（FR-121）：仅按 parent_path 跨库导航，sort 服务端排序；
    // library_id 已弃用、不再下发（后端按真实路径聚合，传了也忽略）。
    const params = { parent_path: parentPath, sort };
    const res = await client.get<BrowseResponse>('/api/library/browse', { params });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '加载目录内容失败，请重试'), { cause: err });
  }
}

async function realAddMediaExtension(
  libraryID: number,
  extension: string,
  type: MediaExtensionType,
): Promise<void> {
  try {
    await client.post('/api/library/extensions', { library_id: libraryID, extension, type });
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '添加后缀失败，请检查格式'), { cause: err });
  }
}

async function realListMediaExtensions(libraryID: number): Promise<MediaExtension[]> {
  const res = await client.get<{ items: MediaExtension[] }>('/api/library/extensions', {
    params: { library_id: libraryID },
  });
  return res.data.items;
}

async function realDeleteMediaExtension(libraryID: number, extension: string): Promise<void> {
  try {
    await client.delete('/api/library/extensions', {
      params: { library_id: libraryID, extension },
    });
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '删除后缀失败'), { cause: err });
  }
}

async function realListMediaTypes(libraryID?: number): Promise<MediaTypesResponse> {
  const res = await client.get<MediaTypesResponse>('/api/media-types', {
    params: libraryID ? { library_id: libraryID } : undefined,
  });
  return res.data;
}

async function realCreateMediaTypeRule(input: CreateMediaTypeRuleInput): Promise<MediaTypeRule> {
  try {
    const res = await client.post<MediaTypeRule>('/api/media-types/rules', input);
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '添加后缀失败，请检查格式'), { cause: err });
  }
}

async function realUpdateMediaTypeRule(
  id: number | string,
  input: UpdateMediaTypeRuleInput,
): Promise<MediaTypeRule> {
  try {
    const res = await client.put<MediaTypeRule>(`/api/media-types/rules/${id}`, input);
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '更新后缀规则失败'), { cause: err });
  }
}

async function realDeleteMediaTypeRule(id: number | string): Promise<void> {
  try {
    await client.delete(`/api/media-types/rules/${id}`);
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '删除后缀失败'), { cause: err });
  }
}

// ─── Mock API 实现 ──────────────────────────────────

async function mockGetLibraryPaths(): Promise<LibraryPath[]> {
  await mockDelay(150);
  // 附带各库已索引媒体数量，与真实接口字段一致
  return mockPaths.map((p) => ({
    ...p,
    media_count: mockMediaFiles.filter((m) => m.library_id === p.id).length,
  }));
}

async function mockGetLibraryKinds(): Promise<LibraryKindInfo[]> {
  await mockDelay(80);
  return defaultLibraryKinds;
}

async function mockCreateLibraryPath(
  path: string,
  type = 'local',
  label = '',
  libraryKind: LibraryKind = 'mixed',
): Promise<LibraryPath> {
  await mockDelay(200);
  const p: LibraryPath = {
    id: nextMockId++,
    path,
    type,
    library_kind: libraryKind,
    library_profile_json: '{}',
    label: label || path,
    enabled: true,
    created_at: new Date().toISOString(),
  };
  mockPaths.push(p);
  return p;
}

async function mockUpdateLibraryPath(
  id: number,
  input: Partial<Pick<LibraryPath, 'label' | 'enabled' | 'library_kind'>>,
): Promise<LibraryPath> {
  await mockDelay(120);
  const path = mockPaths.find((item) => item.id === id);
  if (!path) throw new Error('媒体库不存在');
  if (input.label !== undefined) path.label = input.label.trim();
  if (input.enabled !== undefined) path.enabled = input.enabled;
  if (input.library_kind !== undefined) path.library_kind = input.library_kind;
  return path;
}

async function mockDeleteLibraryPath(id: number): Promise<void> {
  await mockDelay(150);
  const idx = mockPaths.findIndex((p) => p.id === id);
  if (idx !== -1) mockPaths.splice(idx, 1);
  // 清理关联的 mockMediaFiles
  for (let i = mockMediaFiles.length - 1; i >= 0; i--) {
    if (mockMediaFiles[i].library_id === id) mockMediaFiles.splice(i, 1);
  }
  for (let i = mockExtensions.length - 1; i >= 0; i--) {
    if (mockExtensions[i].library_id === id) mockExtensions.splice(i, 1);
  }
  for (let i = mockBuiltinRuleOverrides.length - 1; i >= 0; i--) {
    if (mockBuiltinRuleOverrides[i].library_id === id) mockBuiltinRuleOverrides.splice(i, 1);
  }
}

async function mockGetMediaFiles(params: MediaListParams = {}): Promise<MediaListResponse> {
  await mockDelay(200);
  const { page = 1, page_size = 20, search, sort, favorite, tag_id, inference } = params;
  // 常规列表排除已软删项（FR-25）
  let items = mockMediaFiles.filter((m) => !mockDeletedIds.has(m.id));
  if (search) items = items.filter((m) => m.file_name.toLowerCase().includes(search.toLowerCase()));
  if (favorite) items = items.filter((m) => m.favorite);
  if (inference === 'inferred') items = items.filter((m) => m.inference);
  if (inference === 'auto') items = items.filter((m) => m.inference && !m.inference.manual);
  if (inference === 'manual') items = items.filter((m) => m.inference?.manual);
  if (inference === 'missing') items = items.filter((m) => !m.inference);
  if (tag_id) {
    const ids = new Set(
      mockTagMappings.filter((tm) => tm.tag_id === tag_id).map((tm) => tm.media_id),
    );
    items = items.filter((m) => ids.has(m.id));
  }
  if (sort === 'time_desc') items.sort((a, b) => b.added_at.localeCompare(a.added_at));
  const total = items.length;
  const start = (page - 1) * page_size;
  return { items: items.slice(start, start + page_size), total, page, page_size };
}

// ─── 收藏与标签 mock（FR-41）─────────────────────────

async function mockSetMediaFavorite(id: number, favorite: boolean): Promise<MediaFile> {
  await mockDelay(100);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (!f) throw new Error('媒体文件不存在');
  f.favorite = favorite;
  return f;
}

async function mockGetTags(): Promise<Tag[]> {
  await mockDelay(100);
  return [...mockTags].sort((a, b) => a.name.localeCompare(b.name));
}

async function mockCreateTag(name: string): Promise<Tag> {
  await mockDelay(100);
  const normalized = name.trim();
  if (!normalized) throw new Error('标签名不能为空');
  const existing = mockTags.find((t) => t.name === normalized);
  if (existing) return existing;
  const tag: Tag = { id: nextMockTagId++, name: normalized, created_at: new Date().toISOString() };
  mockTags.push(tag);
  return tag;
}

async function mockGetMediaTags(mediaID: number): Promise<Tag[]> {
  await mockDelay(100);
  const ids = new Set(
    mockTagMappings.filter((tm) => tm.media_id === mediaID).map((tm) => tm.tag_id),
  );
  return mockTags.filter((t) => ids.has(t.id)).sort((a, b) => a.name.localeCompare(b.name));
}

async function mockAddMediaTag(
  mediaID: number,
  tag: { tag_id?: number; name?: string },
): Promise<Tag> {
  await mockDelay(100);
  let resolved: Tag | undefined;
  if (tag.tag_id) resolved = mockTags.find((t) => t.id === tag.tag_id);
  else if (tag.name) resolved = await mockCreateTag(tag.name);
  if (!resolved) throw new Error('标签不存在');
  if (!mockTagMappings.some((tm) => tm.tag_id === resolved!.id && tm.media_id === mediaID)) {
    mockTagMappings.push({ tag_id: resolved.id, media_id: mediaID });
  }
  return resolved;
}

async function mockRemoveMediaTag(mediaID: number, tagID: number): Promise<void> {
  await mockDelay(100);
  const idx = mockTagMappings.findIndex((tm) => tm.tag_id === tagID && tm.media_id === mediaID);
  if (idx !== -1) mockTagMappings.splice(idx, 1);
}

// ─── 续播与观看状态 mock（FR-44）────────────────────

async function mockUpdateWatchPosition(id: number, position: number): Promise<MediaFile> {
  await mockDelay(100);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (!f) throw new Error('媒体文件不存在');
  f.last_position = position < 0 ? 0 : position;
  f.last_watched_at = new Date().toISOString();
  return f;
}

async function mockMarkWatched(id: number): Promise<MediaFile> {
  await mockDelay(100);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (!f) throw new Error('媒体文件不存在');
  f.watched = true;
  f.last_position = 0;
  f.last_watched_at = new Date().toISOString();
  return f;
}

async function mockGetContinueWatching(limit = 12): Promise<MediaFile[]> {
  await mockDelay(100);
  return mockMediaFiles
    .filter((m) => (m.last_position ?? 0) > 0 && !m.watched)
    .sort((a, b) => (b.last_watched_at ?? '').localeCompare(a.last_watched_at ?? ''))
    .slice(0, limit);
}

// 那年今日（FR-72）：挑出 media_time 命中「今天月-日」但年份不等于今年的媒体，按 media_time 倒序。
async function mockGetOnThisDay(limit = 12): Promise<MediaFile[]> {
  await mockDelay(100);
  const now = new Date();
  const monthDay = `${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  const thisYear = now.getFullYear();
  return mockMediaFiles
    .filter((m) => {
      if (!m.media_time) return false;
      const d = new Date(m.media_time);
      const md = `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
      return md === monthDay && d.getFullYear() !== thisYear;
    })
    .sort((a, b) => (b.media_time ?? '').localeCompare(a.media_time ?? ''))
    .slice(0, limit);
}

// ─── 最近查看 mock（FR-120）──────────────────────────

// 记录查看：把该媒体 last_viewed_at 置为当前时间（不存在则静默忽略）。
async function mockSetMediaViewed(id: number): Promise<void> {
  await mockDelay(80);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (f) f.last_viewed_at = new Date().toISOString();
}

// 最近查看列表：last_viewed_at 非空、未软删，按 last_viewed_at 倒序取前 limit 条。
async function mockGetRecentlyViewed(limit = 12): Promise<MediaFile[]> {
  await mockDelay(100);
  return mockMediaFiles
    .filter((m) => !mockDeletedIds.has(m.id) && !!m.last_viewed_at)
    .sort((a, b) => (b.last_viewed_at ?? '').localeCompare(a.last_viewed_at ?? ''))
    .slice(0, limit);
}

async function mockGetMediaFile(id: number): Promise<MediaFile> {
  await mockDelay(100);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (!f) throw new Error('媒体文件不存在');
  return f;
}

async function mockGetMediaMetadata(): Promise<MediaMetadata[]> {
  await mockDelay(80);
  return [];
}

async function mockGetMediaCovers(): Promise<MediaCoversResponse> {
  await mockDelay(80);
  return { cover: null, candidates: [] };
}

async function mockGenerateMediaCovers(): Promise<{ status: string; task_id: number }> {
  await mockDelay(80);
  return { status: 'pending', task_id: nextMockTaskId++ };
}

async function mockSelectMediaCover(): Promise<MediaCover> {
  throw new Error('mock 模式没有可选择的封面候选');
}

async function mockGetMediaInference(id: number): Promise<MediaInference | null> {
  await mockDelay(80);
  return mockInferences.get(id) ?? null;
}

async function mockUpdateMediaInference(
  id: number,
  input: MediaInferenceInput,
): Promise<MediaInference> {
  await mockDelay(120);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (!f) throw new Error('媒体文件不存在');
  const now = new Date().toISOString();
  const inference: MediaInference = {
    id,
    media_id: id,
    space_id: 'space-default',
    kind: input.kind ?? 'mixed',
    title: input.title.trim(),
    year: input.year ?? 0,
    season: input.season ?? 0,
    episode: input.episode ?? 0,
    episode_title: input.episode_title?.trim() ?? '',
    confidence: 1,
    source: 'manual',
    rule_version: 'fr2-031-v1',
    manual: true,
    created_at: mockInferences.get(id)?.created_at ?? now,
    updated_at: now,
  };
  mockInferences.set(id, inference);
  return inference;
}

async function mockBackfillMediaInferences(): Promise<InferenceBackfillAccepted> {
  await mockDelay(200);
  return { task_id: nextMockTaskId++, status: 'pending' };
}

async function mockDeleteMediaFile(id: number): Promise<void> {
  await mockDelay(150);
  // 软删除（FR-25）：仅标记，不从内存数据移除（源文件不动）
  if (mockMediaFiles.some((m) => m.id === id)) mockDeletedIds.add(id);
}

// 批量软删 mock（FR-69）：把存在且未软删的 id 批量加入回收站，返回实际软删条数。
async function mockBatchDeleteMediaFiles(ids: number[]): Promise<number> {
  await mockDelay(150);
  let deleted = 0;
  for (const id of ids) {
    if (mockMediaFiles.some((m) => m.id === id) && !mockDeletedIds.has(id)) {
      mockDeletedIds.add(id);
      deleted++;
    }
  }
  return deleted;
}

// ─── 感知哈希去重 mock（FR-70）──────────────────────

// mock 扫描：无真实哈希计算，恒返回 0（重复组由 mockGetDuplicateGroups 按代理规则现算）。
async function mockScanDuplicates(): Promise<number> {
  await mockDelay(300);
  return 0;
}

// mock 重复组：以 file_size 作为「内容相同」的代理，把未软删、同尺寸且 ≥2 个的媒体聚为一组。
async function mockGetDuplicateGroups(): Promise<DuplicateGroup[]> {
  await mockDelay(200);
  const alive = mockMediaFiles.filter((m) => !mockDeletedIds.has(m.id));
  const bySize = new Map<number, MediaFile[]>();
  for (const m of alive) {
    const arr = bySize.get(m.file_size) ?? [];
    arr.push(m);
    bySize.set(m.file_size, arr);
  }
  return Array.from(bySize.values())
    .filter((g) => g.length >= 2)
    .map((g) => [...g].sort((a, b) => a.id - b.id))
    .sort((a, b) => a[0].id - b[0].id);
}

async function mockBackfillFileHashes(): Promise<FileHashBackfillResponse> {
  await mockDelay(200);
  return { status: 'queued', task_id: String(nextMockTaskId++) };
}

async function mockGetExactDuplicateGroups(): Promise<ExactDuplicateGroup[]> {
  const groups = await mockGetDuplicateGroups();
  return groups.map((items) => ({
    content_hash: `mock-sha256-${items[0].file_size}`,
    file_size: items[0].file_size,
    items,
  }));
}

async function mockGetRecycleMediaFiles(): Promise<MediaFile[]> {
  await mockDelay(150);
  return mockMediaFiles.filter((m) => mockDeletedIds.has(m.id));
}

async function mockRestoreMediaFile(id: number): Promise<void> {
  await mockDelay(150);
  if (!mockDeletedIds.has(id)) throw new Error('回收站中不存在该媒体文件');
  mockDeletedIds.delete(id);
}

// 回收站清理 mock（FR-26）：把全部软删项从内存数据移除（模拟移到回收站目录 + 删记录）。
async function mockCleanupRecycle(): Promise<RecycleCleanupResult> {
  await mockDelay(200);
  let moved = 0;
  for (const id of mockDeletedIds) {
    const idx = mockMediaFiles.findIndex((m) => m.id === id);
    if (idx !== -1) mockMediaFiles.splice(idx, 1);
    moved++;
  }
  mockDeletedIds.clear();
  return { moved, failed: 0 };
}

async function mockRenameMediaFile(id: number, newName: string): Promise<MediaFile> {
  await mockDelay(150);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (!f) throw new Error('媒体文件不存在');
  f.file_name = newName;
  return f;
}

async function mockUpdateDisplayName(id: number, displayName: string): Promise<MediaFile> {
  await mockDelay(120);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (!f) throw new Error('媒体文件不存在');
  f.display_name = displayName.trim();
  return f;
}

async function mockUpdateMediaNotes(id: number, notes: string): Promise<MediaFile> {
  await mockDelay(120);
  const f = mockMediaFiles.find((m) => m.id === id);
  if (!f) throw new Error('媒体文件不存在');
  f.notes = notes.trim();
  return f;
}

async function mockScanLibrary(id: number, _mode: ScanMode = 'incremental'): Promise<ScanResponse> {
  await mockDelay(400);
  // mock 模式仅模拟新增入库，不区分对账（全量对账行为由后端集成测试覆盖）
  const libraryPath = mockPaths.find((p) => p.id === id)?.path || 'D:\\Videos';
  const count = Math.floor(Math.random() * 3) + 1;
  const fmts = ['mp4', 'mkv', 'avi', 'mov'];
  for (let i = 0; i < count; i++) {
    const fileId = nextMockId++;
    const format = fmts[i % fmts.length];
    mockMediaFiles.push({
      id: fileId,
      library_id: id,
      file_path: `${libraryPath}\\scan-${fileId}.${format}`,
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
    });
  }
  // 模拟入队一条已完成的扫描任务（FR-29），供页眉任务展示
  const now = new Date().toISOString();
  mockScanTasks.unshift({
    id: nextMockTaskId++,
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
  return { status: 'queued', task_id: nextMockTaskId - 1 };
}

async function mockGetScanTasks(): Promise<ScanTasksResponse> {
  await mockDelay(80);
  const current = mockScanTasks.find((t) => t.status === 'running') ?? null;
  return { tasks: [...mockScanTasks], current };
}

async function mockAddMediaExtension(
  libraryID: number,
  extension: string,
  type: MediaExtensionType,
): Promise<void> {
  await mockDelay(100);
  const normalized = extension.trim().toLowerCase().replace(/^\./, '');
  if (!normalized) throw new Error('后缀格式不支持');
  if (mockExtensions.some((ext) => ext.library_id === libraryID && ext.extension === normalized))
    return;
  mockExtensions.push({
    id: nextMockExtensionId++,
    library_id: libraryID,
    extension: normalized,
    type,
    is_builtin: 0,
    created_at: new Date().toISOString(),
  });
}

async function mockListMediaExtensions(libraryID: number): Promise<MediaExtension[]> {
  await mockDelay(100);
  return mockExtensions.filter((ext) => ext.library_id === libraryID);
}

async function mockDeleteMediaExtension(libraryID: number, extension: string): Promise<void> {
  await mockDelay(100);
  const normalized = extension.trim().toLowerCase().replace(/^\./, '');
  const idx = mockExtensions.findIndex(
    (ext) => ext.library_id === libraryID && ext.extension === normalized,
  );
  if (idx === -1) throw new Error('自定义后缀不存在');
  mockExtensions.splice(idx, 1);
}

function mockRuleFromExtension(ext: MediaExtension): MediaTypeRule {
  const mediaType = defaultMediaTypes.find((type) => type.type === ext.type);
  return {
    id: ext.id ?? 0,
    space_id: 'space-default',
    library_id: ext.library_id,
    type: ext.type,
    extension: ext.extension,
    label: ext.label || `${ext.extension.toUpperCase()} ${mediaType?.name ?? '媒体'}`,
    description: ext.description || `${ext.extension} ${mediaType?.description ?? '媒体规则'}`,
    enabled: ext.enabled ?? true,
    builtin: ext.builtin ?? Boolean(ext.is_builtin),
    capabilities: ext.capabilities ?? mediaType?.capabilities ?? ['scan'],
  };
}

async function mockListMediaTypes(libraryID?: number): Promise<MediaTypesResponse> {
  await mockDelay(100);
  const rules = [
    ...effectiveBuiltinMockMediaTypeRules(libraryID),
    ...mockExtensions
      .filter((ext) => !libraryID || ext.library_id === libraryID)
      .map((ext) => mockRuleFromExtension(ext)),
  ];
  return { types: defaultMediaTypes, rules };
}

async function mockCreateMediaTypeRule(input: CreateMediaTypeRuleInput): Promise<MediaTypeRule> {
  await mockDelay(100);
  const normalized = input.extension.trim().toLowerCase().replace(/^\./, '');
  if (!normalized) throw new Error('后缀格式不支持');
  const libraryID = input.library_id ?? 0;
  let ext = mockExtensions.find(
    (item) =>
      item.library_id === libraryID && item.extension === normalized && item.type === input.type,
  );
  if (!ext) {
    ext = {
      id: nextMockExtensionId++,
      library_id: libraryID,
      extension: normalized,
      type: input.type,
      is_builtin: 0,
      builtin: false,
      enabled: input.enabled ?? true,
      label: input.label,
      description: input.description,
      created_at: new Date().toISOString(),
    };
    mockExtensions.push(ext);
  }
  return mockRuleFromExtension(ext);
}

async function mockUpdateMediaTypeRule(
  id: number | string,
  input: UpdateMediaTypeRuleInput,
): Promise<MediaTypeRule> {
  await mockDelay(100);
  const ruleID = String(id);
  const ext = mockExtensions.find((item) => String(item.id) === ruleID);
  if (ext) {
    if (input.enabled !== undefined) ext.enabled = input.enabled;
    if (input.label !== undefined) ext.label = input.label.trim();
    if (input.description !== undefined) ext.description = input.description.trim();
    return mockRuleFromExtension(ext);
  }
  const builtin = builtinMockMediaTypeRules(input.library_id).find(
    (rule) => String(rule.id) === ruleID,
  );
  if (!builtin) throw new Error('规则不存在');
  const updated = {
    ...builtin,
    enabled: input.enabled ?? builtin.enabled,
    label: input.label ?? builtin.label,
    description: input.description ?? builtin.description,
  };
  const key = mockMediaRuleKey(updated);
  const idx = mockBuiltinRuleOverrides.findIndex((rule) => mockMediaRuleKey(rule) === key);
  if (idx >= 0) {
    mockBuiltinRuleOverrides[idx] = updated;
  } else {
    mockBuiltinRuleOverrides.push(updated);
  }
  return updated;
}

async function mockDeleteMediaTypeRule(id: number | string): Promise<void> {
  await mockDelay(100);
  const idx = mockExtensions.findIndex((item) => String(item.id) === String(id));
  if (idx === -1) throw new Error('自定义后缀不存在');
  mockExtensions.splice(idx, 1);
}

// 推导真实路径的卷根（FR-121）：本地 `D:/...` → `D:`；UNC `//host/share/...` → `//host/share`。
function mockVolumeRoot(rawPath: string): string {
  const p = rawPath.replace(/\\/g, '/');
  if (p.startsWith('//')) {
    const seg = p.slice(2).split('/').filter(Boolean);
    return seg.length >= 2 ? `//${seg[0]}/${seg[1]}` : p;
  }
  const m = p.match(/^([A-Za-z]:)/);
  return m ? m[1] : p.split('/').filter(Boolean)[0] || p;
}

// 按 sort 对文件排序（与后端语义一致：目录恒在前另行处理，此处仅排文件，全部升序）。
function mockSortFiles(files: MediaFile[], sort: string): MediaFile[] {
  const arr = [...files];
  switch (sort) {
    case 'size':
      return arr.sort((a, b) => a.file_size - b.file_size);
    case 'type':
      return arr.sort(
        (a, b) =>
          (a.format || '').localeCompare(b.format || '') || a.file_name.localeCompare(b.file_name),
      );
    case 'time':
      return arr.sort((a, b) => (a.modified_at || '').localeCompare(b.modified_at || ''));
    default:
      return arr.sort((a, b) => a.file_name.localeCompare(b.file_name));
  }
}

async function mockBrowseDirectory(parentPath: string, sort = 'name'): Promise<BrowseResponse> {
  await mockDelay(150);
  const alive = mockMediaFiles.filter((m) => !mockDeletedIds.has(m.id));

  // 真实路径树根（FR-121）：各启用库推导卷根、去重排序作为顶层目录项（不带 library_id）。
  if (parentPath === '__root__') {
    const roots = Array.from(
      new Set(mockPaths.filter((p) => p.enabled).map((p) => mockVolumeRoot(p.path))),
    ).sort();
    return {
      breadcrumbs: [{ name: '全部', path: '__root__' }],
      directories: roots.map((r) => ({ name: r, path: r })),
      files: [],
    };
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

  // 面包屑：按分隔符拆段累进，Windows 盘符不加前导斜杠（保持 `D:/...` 形式）。
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

  return {
    breadcrumbs,
    directories: Array.from(dirSet)
      .sort()
      .map((name) => ({ name, path: prefix + name })),
    files: mockSortFiles(files, sort),
  };
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getLibraryPaths() {
  return useMock ? mockGetLibraryPaths() : realGetLibraryPaths();
}
export function getLibraryKinds() {
  return useMock ? mockGetLibraryKinds() : realGetLibraryKinds();
}
export function createLibraryPath(p: string, t = 'local', l = '', kind: LibraryKind = 'mixed') {
  return useMock ? mockCreateLibraryPath(p, t, l, kind) : realCreateLibraryPath(p, t, l, kind);
}
export function updateLibraryPath(
  id: number,
  input: Partial<Pick<LibraryPath, 'label' | 'enabled' | 'library_kind'>>,
) {
  return useMock ? mockUpdateLibraryPath(id, input) : realUpdateLibraryPath(id, input);
}
export function deleteLibraryPath(id: number) {
  return useMock ? mockDeleteLibraryPath(id) : realDeleteLibraryPath(id);
}
export function getMediaFiles(params?: MediaListParams) {
  return useMock ? mockGetMediaFiles(params) : realGetMediaFiles(params);
}
export function getMediaFile(id: number, signal?: AbortSignal) {
  return useMock ? mockGetMediaFile(id) : realGetMediaFile(id, signal);
}
export function getMediaMetadata(id: number) {
  return useMock ? mockGetMediaMetadata() : realGetMediaMetadata(id);
}
export function getMediaCovers(id: number) {
  return useMock ? mockGetMediaCovers() : realGetMediaCovers(id);
}
export function generateMediaCovers(id: number, refresh: boolean) {
  return useMock ? mockGenerateMediaCovers() : realGenerateMediaCovers(id, refresh);
}
export function selectMediaCover(id: number, candidateID: number) {
  return useMock ? mockSelectMediaCover() : realSelectMediaCover(id, candidateID);
}
export function getMediaInference(id: number, signal?: AbortSignal) {
  return useMock ? mockGetMediaInference(id) : realGetMediaInference(id, signal);
}
export function updateMediaInference(id: number, input: MediaInferenceInput) {
  return useMock ? mockUpdateMediaInference(id, input) : realUpdateMediaInference(id, input);
}
export function backfillMediaInferences(libraryID?: number) {
  return useMock ? mockBackfillMediaInferences() : realBackfillMediaInferences(libraryID);
}
export function deleteMediaFile(id: number) {
  return useMock ? mockDeleteMediaFile(id) : realDeleteMediaFile(id);
}
// 批量软删（FR-69）
export function batchDeleteMediaFiles(ids: number[]) {
  return useMock ? mockBatchDeleteMediaFiles(ids) : realBatchDeleteMediaFiles(ids);
}
// 感知哈希去重（FR-70）
export function scanDuplicates() {
  return useMock ? mockScanDuplicates() : realScanDuplicates();
}
export function getDuplicateGroups() {
  return useMock ? mockGetDuplicateGroups() : realGetDuplicateGroups();
}
export function backfillFileHashes() {
  return useMock ? mockBackfillFileHashes() : realBackfillFileHashes();
}
export function getExactDuplicateGroups() {
  return useMock ? mockGetExactDuplicateGroups() : realGetExactDuplicateGroups();
}
export function renameMediaFile(id: number, newName: string) {
  return useMock ? mockRenameMediaFile(id, newName) : realRenameMediaFile(id, newName);
}
export function updateMediaNotes(id: number, notes: string) {
  return useMock ? mockUpdateMediaNotes(id, notes) : realUpdateMediaNotes(id, notes);
}

export function updateDisplayName(id: number, displayName: string) {
  return useMock ? mockUpdateDisplayName(id, displayName) : realUpdateDisplayName(id, displayName);
}
// 软删除与回收站（FR-25）
export function getRecycleMediaFiles() {
  return useMock ? mockGetRecycleMediaFiles() : realGetRecycleMediaFiles();
}
export function restoreMediaFile(id: number) {
  return useMock ? mockRestoreMediaFile(id) : realRestoreMediaFile(id);
}
// 回收站清理（FR-26）
export function cleanupRecycle() {
  return useMock ? mockCleanupRecycle() : realCleanupRecycle();
}
export function scanLibrary(id: number, mode: ScanMode = 'incremental') {
  return useMock ? mockScanLibrary(id, mode) : realScanLibrary(id, mode);
}
// Web 上传入库（FR-149）：mock 模式下直接回成功，便于本地无后端联调
export function uploadMedia(file: File, opts?: UploadMediaOptions): Promise<UploadResponse> {
  if (useMock) {
    return Promise.resolve({
      status: 'uploaded',
      library_id: 1,
      file_path: `mock/${file.name}`,
      scan_task: 0,
    });
  }
  return realUploadMedia(file, opts);
}
// 扫描任务队列（FR-29）
export function getScanTasks() {
  return useMock ? mockGetScanTasks() : realGetScanTasks();
}

/**
 * 创建扫描进度 SSE 连接，返回关闭函数。
 * mock 模式或运行环境无 EventSource（如测试 jsdom）时立即返回空关闭函数。
 */
export function createScanProgressSSE(onProgress: (status: ScanStatus) => void): () => void {
  if (useMock || typeof EventSource === 'undefined') {
    return () => {};
  }
  const eventSource = new EventSource('/api/library/scan/progress');
  eventSource.addEventListener('progress', (e) => {
    onProgress(JSON.parse(e.data) as ScanStatus);
  });
  return () => eventSource.close();
}
// 目录浏览（FR-121）：仅按真实路径 parent_path 跨库导航，sort 服务端排序（name/size/type/time）。
export function browseDirectory(parentPath: string, sort = 'name') {
  return useMock ? mockBrowseDirectory(parentPath, sort) : realBrowseDirectory(parentPath, sort);
}

// 收藏与标签（FR-41）
export function setMediaFavorite(id: number, favorite: boolean) {
  return useMock ? mockSetMediaFavorite(id, favorite) : realSetMediaFavorite(id, favorite);
}
export function getTags() {
  return useMock ? mockGetTags() : realGetTags();
}
export function createTag(name: string) {
  return useMock ? mockCreateTag(name) : realCreateTag(name);
}
export function getMediaTags(mediaID: number) {
  return useMock ? mockGetMediaTags(mediaID) : realGetMediaTags(mediaID);
}
export function addMediaTag(mediaID: number, tag: { tag_id?: number; name?: string }) {
  return useMock ? mockAddMediaTag(mediaID, tag) : realAddMediaTag(mediaID, tag);
}
export function removeMediaTag(mediaID: number, tagID: number) {
  return useMock ? mockRemoveMediaTag(mediaID, tagID) : realRemoveMediaTag(mediaID, tagID);
}

// 续播与观看状态（FR-44）
export function updateWatchPosition(id: number, position: number) {
  return useMock ? mockUpdateWatchPosition(id, position) : realUpdateWatchPosition(id, position);
}
export function markWatched(id: number) {
  return useMock ? mockMarkWatched(id) : realMarkWatched(id);
}
export function getContinueWatching(limit = 12) {
  return useMock ? mockGetContinueWatching(limit) : realGetContinueWatching(limit);
}
// 那年今日（FR-72）：往年同一天拍摄的媒体回忆列表
export function getOnThisDay(limit = 12) {
  return useMock ? mockGetOnThisDay(limit) : realGetOnThisDay(limit);
}
// 最近查看（FR-120）：记录媒体打开时间 + 拉取最近查看列表
export function setMediaViewed(id: number, signal?: AbortSignal) {
  return useMock ? mockSetMediaViewed(id) : realSetMediaViewed(id, signal);
}
export function getRecentlyViewed(limit = 12) {
  return useMock ? mockGetRecentlyViewed(limit) : realGetRecentlyViewed(limit);
}
export function addMediaExtension(libraryID: number, extension: string, type: MediaExtensionType) {
  return useMock
    ? mockAddMediaExtension(libraryID, extension, type)
    : realAddMediaExtension(libraryID, extension, type);
}
export function listMediaExtensions(libraryID: number) {
  return useMock ? mockListMediaExtensions(libraryID) : realListMediaExtensions(libraryID);
}
export function deleteMediaExtension(libraryID: number, extension: string) {
  return useMock
    ? mockDeleteMediaExtension(libraryID, extension)
    : realDeleteMediaExtension(libraryID, extension);
}
export function listMediaTypes(libraryID?: number) {
  return useMock ? mockListMediaTypes(libraryID) : realListMediaTypes(libraryID);
}
export function createMediaTypeRule(input: CreateMediaTypeRuleInput) {
  return useMock ? mockCreateMediaTypeRule(input) : realCreateMediaTypeRule(input);
}
export function updateMediaTypeRule(id: number | string, input: UpdateMediaTypeRuleInput) {
  return useMock ? mockUpdateMediaTypeRule(id, input) : realUpdateMediaTypeRule(id, input);
}
export function deleteMediaTypeRule(id: number | string) {
  return useMock ? mockDeleteMediaTypeRule(id) : realDeleteMediaTypeRule(id);
}
