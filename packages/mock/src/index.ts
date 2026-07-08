export type MockScenarioId =
  | 'empty-library'
  | 'normal-library'
  | 'million-assets'
  | 'missing-thumbnail'
  | 'hls-pending'
  | 'transcode-failed'
  | 'permission-denied'
  | 'ai-review-pending';

export type MockSurface =
  | 'hls-preview'
  | 'thumbnail'
  | 'transcode-task'
  | 'ai-task'
  | 'space-permission'
  | 'pixi-grid';

export interface MockScenario {
  readonly id: MockScenarioId;
  readonly title: string;
  readonly dataset: 'smoke' | 'target-1m' | 'index-5m' | 'index-10m';
  readonly summary: string;
  readonly surfaces: readonly MockSurface[];
  readonly sampleAssetPath: string;
}

export type MockMediaDataset = 'media-index-1m' | 'media-index-5m' | 'media-index-10m';

export type MockMediaType = 'image' | 'video';

export type MockTranscodeStatus = 'failed' | 'pending' | 'ready';

export type MockAiStatus = 'indexed' | 'pending' | 'review';

export interface MockMediaRecord {
  readonly id: string;
  readonly position: number;
  readonly spaceId: string;
  readonly path: string;
  readonly capturedAt: string;
  readonly type: MockMediaType;
  readonly durationSeconds: number;
  readonly transcodeStatus: MockTranscodeStatus;
  readonly aiStatus: MockAiStatus;
  readonly hasThumbnail: boolean;
  readonly hlsStatus: 'pending' | 'ready';
}

export interface MockMediaIndexOptions {
  readonly dataset: MockMediaDataset;
  readonly seed: string;
}

export interface MockMediaWindowQuery {
  readonly limit: number;
  readonly offset: number;
  readonly pathPrefix?: string;
  readonly spaceId?: string;
  readonly type?: MockMediaType;
}

export interface MockMediaWindowResult {
  readonly items: readonly MockMediaRecord[];
  readonly scannedRows: number;
  readonly total: number;
}

export interface MockMediaIndex {
  readonly total: number;
  readonly residentObjectCount: number;
  get(position: number): MockMediaRecord;
  queryWindow(query: MockMediaWindowQuery): MockMediaWindowResult;
}

export const mockScenarios: readonly MockScenario[] = [
  {
    id: 'empty-library',
    title: '空媒体库',
    dataset: 'smoke',
    summary: '空态用于验证媒体库、任务队列和 Space 权限面板的无数据展示。',
    surfaces: ['thumbnail', 'space-permission'],
    sampleAssetPath: '/mock/media/empty',
  },
  {
    id: 'normal-library',
    title: '正常媒体库',
    dataset: 'smoke',
    summary: '正常库包含可播放 HLS 预览、可用缩略图和已完成任务。',
    surfaces: ['hls-preview', 'thumbnail', 'transcode-task'],
    sampleAssetPath: '/mock/media/normal-library/demo.mp4',
  },
  {
    id: 'million-assets',
    title: '百万素材压力场景',
    dataset: 'target-1m',
    summary: '百万素材压力场景用于展示 PixiJS 可见窗口与纹理指标入口。',
    surfaces: ['pixi-grid', 'thumbnail'],
    sampleAssetPath: '/mock/media/million-assets/asset-000001.mp4',
  },
  {
    id: 'missing-thumbnail',
    title: '缩略图缺失',
    dataset: 'smoke',
    summary: '缩略图缺失场景展示待生成、失败和降级占位状态。',
    surfaces: ['thumbnail'],
    sampleAssetPath: '/mock/media/missing-thumbnail/clip.mp4',
  },
  {
    id: 'hls-pending',
    title: 'HLS 生成中',
    dataset: 'smoke',
    summary: 'HLS 生成中场景展示预览卡等待切片、可重试与不可播放状态。',
    surfaces: ['hls-preview', 'transcode-task'],
    sampleAssetPath: '/mock/media/hls-pending/source.mov',
  },
  {
    id: 'transcode-failed',
    title: '转码失败',
    dataset: 'smoke',
    summary: '转码失败场景展示任务错误、失败原因摘要和重试入口。',
    surfaces: ['transcode-task', 'hls-preview'],
    sampleAssetPath: '/mock/media/transcode-failed/source.mkv',
  },
  {
    id: 'permission-denied',
    title: '权限不足',
    dataset: 'smoke',
    summary: 'Space 权限不足场景展示只读、不可删除和 AI 不可见状态。',
    surfaces: ['space-permission'],
    sampleAssetPath: '/mock/media/space-denied/private-demo.mp4',
  },
  {
    id: 'ai-review-pending',
    title: 'AI 审核待处理',
    dataset: 'smoke',
    summary: 'AI 审核待处理场景展示对象识别、OCR 和人工确认等待状态。',
    surfaces: ['ai-task', 'space-permission'],
    sampleAssetPath: '/mock/media/ai-review/clip.mp4',
  },
] as const;

export function findScenario(id: MockScenarioId): MockScenario {
  const scenario = mockScenarios.find((item) => item.id === id);
  if (!scenario) {
    throw new Error(`未知 mock 场景：${id}`);
  }
  return scenario;
}

type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

interface MockRequestLike {
  readonly headers: Pick<Headers, 'get'>;
  readonly method: string;
  readonly url: string;
}

interface MockMediaItem {
  readonly created_at: string;
  readonly duration_seconds: number;
  readonly id: string;
  readonly kind: 'video' | 'image';
  readonly space_id: string;
  readonly title: string;
}

interface MockTaskItem {
  readonly created_at: string;
  readonly error: string | null;
  readonly id: string;
  readonly priority: number;
  readonly progress: number;
  readonly space_id: string;
  readonly status: 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'completed' | 'error';
  readonly type: 'scan' | 'transcode' | 'thumbnail' | 'preview' | 'metadata' | 'export' | 'ai';
  readonly updated_at: string;
}

type MockTaskStatus = MockTaskItem['status'];

type MockTaskType = MockTaskItem['type'];

const mediaFixtures: readonly MockMediaItem[] = [
  {
    created_at: '2026-07-01T10:00:00Z',
    duration_seconds: 120,
    id: 'media-family-001',
    kind: 'video',
    space_id: 'space-default',
    title: '家庭素材 001',
  },
  {
    created_at: '2026-07-01T11:00:00Z',
    duration_seconds: 240,
    id: 'media-family-002',
    kind: 'video',
    space_id: 'space-default',
    title: '家庭素材 002',
  },
  {
    created_at: '2026-07-02T10:00:00Z',
    duration_seconds: 90,
    id: 'media-studio-001',
    kind: 'video',
    space_id: 'space-studio',
    title: '工作室素材 001',
  },
] as const;

const taskSequences = new Map<string, readonly MockTaskItem[]>([
  [
    'task-transcode-default',
    [
      createTask('task-transcode-default', 'space-default', 'running', 0.5),
      createTask('task-transcode-default', 'space-default', 'succeeded', 1),
    ],
  ],
  ['task-legacy-completed', [createTask('task-legacy-completed', 'space-default', 'completed', 1)]],
]);

const taskFixtures: readonly MockTaskItem[] = [
  createTask('task-transcode-default', 'space-default', 'running', 0.5),
  createTask('task-legacy-completed', 'space-default', 'completed', 1),
  createTask('task-transcode-failed', 'space-default', 'failed', 0.4, 'transcode', '编码器不可用'),
  createTask('task-scan-pending', 'space-default', 'pending', 0, 'scan'),
  createTask('task-thumbnail-done', 'space-default', 'succeeded', 1, 'thumbnail'),
  createTask('task-studio-transcode', 'space-studio', 'running', 0.2),
] as const;

export function createMockFetch(): FetchLike {
  const taskReads = new Map<string, number>();
  const taskOverrides = new Map<string, MockTaskItem>();
  return (input, init) => Promise.resolve(handleMockApiRequest(toMockRequest(input, init), taskReads, taskOverrides));
}

export function handleMockApiRequest(
  request: MockRequestLike,
  taskReads = new Map<string, number>(),
  taskOverrides = new Map<string, MockTaskItem>(),
): Response {
  const url = new URL(request.url);
  const method = request.method.toUpperCase();
  const spaceId = url.searchParams.get('space_id') ?? request.headers.get('X-JianVideo-Space-Id') ?? 'space-default';

  if (method === 'GET' && url.pathname === '/api/v2/media') {
    return mediaListResponse(url, spaceId);
  }
  if (method === 'GET' && url.pathname.startsWith('/api/v2/media/')) {
    return mediaDetailResponse(url.pathname.slice('/api/v2/media/'.length), spaceId);
  }
  if (method === 'GET' && url.pathname === '/api/tasks') {
    return taskListResponse(url, spaceId, taskOverrides);
  }
  if (method === 'GET' && url.pathname === '/api/tasks/stats') {
    return taskStatsResponse(url, spaceId, taskOverrides);
  }
  if (method === 'GET' && url.pathname.startsWith('/api/tasks/')) {
    return taskDetailResponse(url.pathname.slice('/api/tasks/'.length), spaceId, taskReads, taskOverrides);
  }
  if (method === 'POST' && url.pathname.startsWith('/api/tasks/')) {
    return taskActionResponse(url.pathname.slice('/api/tasks/'.length), spaceId, taskReads, taskOverrides);
  }
  if (method === 'GET' && url.pathname.startsWith('/api/v2/tasks/')) {
    return taskDetailResponse(url.pathname.slice('/api/v2/tasks/'.length), spaceId, taskReads, taskOverrides);
  }
  return errorResponse(404, 'MOCK_NOT_FOUND', 'mock 接口不存在');
}

function mediaListResponse(url: URL, spaceId: string): Response {
  const page = toPositiveInt(url.searchParams.get('page'), 1);
  const pageSize = toPositiveInt(url.searchParams.get('page_size'), 20);
  const items = mediaFixtures.filter((item) => item.space_id === spaceId);
  const start = (page - 1) * pageSize;
  return Response.json({
    items: items.slice(start, start + pageSize),
    page,
    page_size: pageSize,
    total: items.length,
  });
}

function mediaDetailResponse(id: string, spaceId: string): Response {
  const item = mediaFixtures.find((media) => media.id === id && media.space_id === spaceId);
  if (item === undefined) {
    return errorResponse(404, 'MEDIA_NOT_FOUND', '媒体不存在');
  }
  return Response.json(item);
}

function taskListResponse(
  url: URL,
  spaceId: string,
  taskOverrides: Map<string, MockTaskItem>,
): Response {
  const page = toPositiveInt(url.searchParams.get('page'), 1);
  const pageSize = toPositiveInt(url.searchParams.get('page_size'), 20);
  const type = url.searchParams.get('type');
  const status = url.searchParams.get('status');
  const items = visibleTasks(taskOverrides).filter((task) => {
    if (task.space_id !== spaceId) {
      return false;
    }
    if (type !== null && task.type !== type) {
      return false;
    }
    if (status !== null && normalizeMockTaskStatus(task.status) !== status) {
      return false;
    }
    return true;
  });
  const start = (page - 1) * pageSize;
  return Response.json({
    items: items.slice(start, start + pageSize),
    page,
    page_size: pageSize,
    total: items.length,
  });
}

function taskDetailResponse(
  id: string,
  spaceId: string,
  taskReads: Map<string, number>,
  taskOverrides: Map<string, MockTaskItem>,
): Response {
  const task = currentTask(id, taskReads, taskOverrides);
  if (task === undefined || task.space_id !== spaceId) {
    return errorResponse(404, 'TASK_NOT_FOUND', '任务不存在');
  }
  return Response.json(task);
}

function taskStatsResponse(
  url: URL,
  spaceId: string,
  taskOverrides: Map<string, MockTaskItem>,
): Response {
  const type = url.searchParams.get('type');
  const status = url.searchParams.get('status');
  const items = visibleTasks(taskOverrides).filter((task) => {
    if (task.space_id !== spaceId) {
      return false;
    }
    if (type !== null && task.type !== type) {
      return false;
    }
    if (status !== null && normalizeMockTaskStatus(task.status) !== status) {
      return false;
    }
    return true;
  });
  return Response.json({
    by_status: countBy(items, (task) => normalizeMockTaskStatus(task.status)),
    by_type: countBy(items, (task) => task.type),
    total: items.length,
  });
}

function taskActionResponse(
  path: string,
  spaceId: string,
  taskReads: Map<string, number>,
  taskOverrides: Map<string, MockTaskItem>,
): Response {
  const [id, action] = path.split('/');
  if (id === undefined || action === undefined) {
    return errorResponse(404, 'TASK_NOT_FOUND', '任务不存在');
  }
  const task = currentTask(id, taskReads, taskOverrides);
  if (task === undefined || task.space_id !== spaceId) {
    return errorResponse(404, 'TASK_NOT_FOUND', '任务不存在');
  }
  if (action === 'cancel') {
    const canceled = updateTask(task, { status: 'canceled' });
    taskOverrides.set(id, canceled);
    return Response.json(canceled);
  }
  if (action === 'retry') {
    const retried = updateTask(task, { error: null, progress: 0, status: 'pending' });
    taskOverrides.set(id, retried);
    return Response.json(retried);
  }
  return errorResponse(404, 'TASK_NOT_FOUND', '任务不存在');
}

function currentTask(
  id: string,
  taskReads: Map<string, number>,
  taskOverrides: Map<string, MockTaskItem>,
): MockTaskItem | undefined {
  const override = taskOverrides.get(id);
  if (override !== undefined) {
    return override;
  }
  const sequence = taskSequences.get(id);
  if (sequence === undefined) {
    return taskFixtures.find((task) => task.id === id);
  }
  const readCount = taskReads.get(id) ?? 0;
  taskReads.set(id, readCount + 1);
  return sequence[Math.min(readCount, sequence.length - 1)];
}

function visibleTasks(taskOverrides: Map<string, MockTaskItem>): readonly MockTaskItem[] {
  return taskFixtures.map((task) => taskOverrides.get(task.id) ?? task);
}

function updateTask(task: MockTaskItem, patch: Partial<MockTaskItem>): MockTaskItem {
  return { ...task, ...patch, updated_at: '2026-07-01T10:02:00Z' };
}

function normalizeMockTaskStatus(status: MockTaskStatus): Exclude<MockTaskStatus, 'completed' | 'error'> {
  if (status === 'completed') {
    return 'succeeded';
  }
  if (status === 'error') {
    return 'failed';
  }
  return status;
}

function countBy<T, K extends string>(items: readonly T[], keyOf: (item: T) => K): Partial<Record<K, number>> {
  return items.reduce<Partial<Record<K, number>>>((result, item) => {
    const key = keyOf(item);
    result[key] = (result[key] ?? 0) + 1;
    return result;
  }, {});
}

function createTask(
  id: string,
  spaceId: string,
  status: MockTaskItem['status'],
  progress: number,
  type: MockTaskType = 'transcode',
  error: string | null = null,
): MockTaskItem {
  return {
    created_at: '2026-07-01T10:00:00Z',
    error,
    id,
    priority: 10,
    progress,
    space_id: spaceId,
    status,
    type,
    updated_at: '2026-07-01T10:00:02Z',
  };
}

function toPositiveInt(value: string | null, fallback: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function errorResponse(status: number, code: string, message: string): Response {
  return Response.json({ code, message }, { status });
}

function toMockRequest(input: RequestInfo | URL, init: RequestInit | undefined): MockRequestLike {
  const requestInput = getRequestLike(input);
  return {
    headers: new Headers(init?.headers ?? requestInput?.headers),
    method: init?.method ?? requestInput?.method ?? 'GET',
    url: toRequestUrl(input),
  };
}

function toRequestUrl(input: RequestInfo | URL): string {
  if (typeof input === 'string') {
    return input;
  }
  if (isUrlLike(input)) {
    return input.href;
  }
  return input.url;
}

function isUrlLike(input: Request | URL): input is URL {
  return 'href' in input;
}

function getRequestLike(input: RequestInfo | URL): Pick<Request, 'headers' | 'method' | 'url'> | undefined {
  if (typeof input === 'string' || isUrlLike(input)) {
    return undefined;
  }
  return input;
}

export function scanMockScenarioForSensitiveInfo(): readonly string[] {
  const values = mockScenarios.flatMap((scenario) => [
    scenario.id,
    scenario.title,
    scenario.dataset,
    scenario.summary,
    scenario.sampleAssetPath,
    ...scenario.surfaces,
  ]);
  return values.filter(hasSensitivePattern);
}

function hasSensitivePattern(value: string): boolean {
  return [
    /[A-Za-z]:[\\/]/,
    /\/Users\/|\/home\//,
    /\b(?:10|127|172\.(?:1[6-9]|2\d|3[0-1])|192\.168)\.\d{1,3}\.\d{1,3}\b/,
    /\b(?:password|secret|token|apikey|api_key)\b/i,
    /@[^/]+\.[^/]+/,
  ].some((pattern) => pattern.test(value));
}

export function createMockMediaIndex(options: MockMediaIndexOptions): MockMediaIndex {
  const total = resolveDatasetSize(options.dataset);
  let residentObjectCount = 0;

  return {
    total,
    get residentObjectCount() {
      return residentObjectCount;
    },
    get(position: number) {
      return createRecord(total, options.seed, position);
    },
    queryWindow(query: MockMediaWindowQuery) {
      const items: MockMediaRecord[] = [];
      const limit = Math.max(0, query.limit);
      const offset = Math.max(0, query.offset);
      let matched = 0;
      let scannedRows = 0;

      for (let position = 0; position < total && items.length < limit; position += 1) {
        const record = createRecord(total, options.seed, position);
        scannedRows += 1;
        if (!matchesQuery(record, query)) {
          continue;
        }
        if (matched < offset) {
          matched += 1;
          continue;
        }
        matched += 1;
        items.push(record);
      }

      residentObjectCount = items.length;
      return { items, scannedRows, total };
    },
  };
}

function resolveDatasetSize(dataset: MockMediaDataset): number {
  if (dataset === 'media-index-1m') {
    return 1_000_000;
  }
  if (dataset === 'media-index-5m') {
    return 5_000_000;
  }
  return 10_000_000;
}

function createRecord(total: number, seed: string, position: number): MockMediaRecord {
  const hash = hashSeed(seed);
  const normalized = ((position % total) + total) % total;
  const shifted = normalized + hash;
  const spaceNumber = (shifted % 10) + 1;
  const spaceId = `space-${spaceNumber.toString()}`;
  const type: MockMediaType = shifted % 4 === 0 ? 'image' : 'video';
  const year = 2020 + (shifted % 7);
  const month = (shifted % 12) + 1;
  const day = (shifted % 28) + 1;
  const directory = shifted % 1_000;

  return {
    aiStatus: resolveAiStatus(shifted),
    capturedAt: `${year.toString()}-${pad2(month)}-${pad2(day)}T00:00:00.000Z`,
    durationSeconds: type === 'video' ? 15 + (shifted % 7_200) : 0,
    hasThumbnail: shifted % 17 !== 0,
    hlsStatus: shifted % 5 === 0 ? 'pending' : 'ready',
    id: `media-${hash.toString(36)}-${normalized.toString(36)}`,
    path: `/space-${spaceNumber.toString()}/library-${directory.toString().padStart(4, '0')}/asset-${normalized.toString().padStart(8, '0')}.${type === 'video' ? 'mp4' : 'jpg'}`,
    position: normalized,
    spaceId,
    transcodeStatus: resolveTranscodeStatus(shifted),
    type,
  };
}

function matchesQuery(record: MockMediaRecord, query: MockMediaWindowQuery): boolean {
  if (query.spaceId !== undefined && record.spaceId !== query.spaceId) {
    return false;
  }
  if (query.type !== undefined && record.type !== query.type) {
    return false;
  }
  if (query.pathPrefix !== undefined && !record.path.startsWith(query.pathPrefix)) {
    return false;
  }
  return true;
}

function resolveTranscodeStatus(value: number): MockTranscodeStatus {
  if (value % 19 === 0) {
    return 'failed';
  }
  if (value % 5 === 0) {
    return 'pending';
  }
  return 'ready';
}

function resolveAiStatus(value: number): MockAiStatus {
  if (value % 23 === 0) {
    return 'review';
  }
  if (value % 7 === 0) {
    return 'pending';
  }
  return 'indexed';
}

function hashSeed(seed: string): number {
  let hash = 2_166_136_261;
  for (const char of seed) {
    hash ^= char.codePointAt(0) ?? 0;
    hash = Math.imul(hash, 16_777_619);
  }
  return hash >>> 0;
}

function pad2(value: number): string {
  return value.toString().padStart(2, '0');
}
