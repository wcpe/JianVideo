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

export function createMockFetch(): FetchLike {
  const taskReads = new Map<string, number>();
  return (input, init) => Promise.resolve(handleMockApiRequest(toMockRequest(input, init), taskReads));
}

export function handleMockApiRequest(request: MockRequestLike, taskReads = new Map<string, number>()): Response {
  const url = new URL(request.url);
  const spaceId = request.headers.get('X-JianVideo-Space-Id') ?? 'space-default';

  if (request.method === 'GET' && url.pathname === '/api/v2/media') {
    return mediaListResponse(url, spaceId);
  }
  if (request.method === 'GET' && url.pathname.startsWith('/api/v2/media/')) {
    return mediaDetailResponse(url.pathname.slice('/api/v2/media/'.length), spaceId);
  }
  if (request.method === 'GET' && url.pathname.startsWith('/api/v2/tasks/')) {
    return taskDetailResponse(url.pathname.slice('/api/v2/tasks/'.length), spaceId, taskReads);
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

function taskDetailResponse(id: string, spaceId: string, taskReads: Map<string, number>): Response {
  const sequence = taskSequences.get(id);
  if (sequence === undefined || sequence[0]?.space_id !== spaceId) {
    return errorResponse(404, 'TASK_NOT_FOUND', '任务不存在');
  }
  const readCount = taskReads.get(id) ?? 0;
  taskReads.set(id, readCount + 1);
  return Response.json(sequence[Math.min(readCount, sequence.length - 1)]);
}

function createTask(id: string, spaceId: string, status: MockTaskItem['status'], progress: number): MockTaskItem {
  return {
    created_at: '2026-07-01T10:00:00Z',
    error: null,
    id,
    priority: 10,
    progress,
    space_id: spaceId,
    status,
    type: 'transcode',
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
