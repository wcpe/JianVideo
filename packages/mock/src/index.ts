export type MockScenarioId =
  | 'empty-library'
  | 'normal-library'
  | 'million-assets'
  | 'missing-thumbnail'
  | 'hls-pending'
  | 'transcode-failed'
  | 'permission-denied'
  | 'ai-review-pending';

export interface MockScenario {
  readonly id: MockScenarioId;
  readonly title: string;
  readonly dataset: 'smoke' | 'target-1m' | 'index-5m' | 'index-10m';
}

export const mockScenarios: readonly MockScenario[] = [
  { id: 'empty-library', title: '空媒体库', dataset: 'smoke' },
  { id: 'normal-library', title: '正常媒体库', dataset: 'smoke' },
  { id: 'million-assets', title: '百万素材压力场景', dataset: 'target-1m' },
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
