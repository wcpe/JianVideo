export type TaskState = 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';
export type DevicePlatform = 'web' | 'desktop' | 'mobile' | 'tv' | 'automotive';
export type PointerCapability = 'fine' | 'coarse';
export type NetworkCapability = 'offline' | 'constrained' | 'standard' | 'fast';

export interface SpaceContext {
  readonly spaceId: string;
}

export interface DeviceCapabilities {
  readonly platform: DevicePlatform;
  readonly pointer: PointerCapability;
  readonly touch: boolean;
  readonly network: NetworkCapability;
}

export interface PageParams {
  readonly page: number;
  readonly pageSize: number;
}

export interface MediaItem {
  readonly id: string;
  readonly spaceId: string;
  readonly title: string;
  readonly kind: 'video' | 'image';
  readonly durationSeconds: number;
  readonly createdAt: string;
}

export interface PageResult<T> {
  readonly items: readonly T[];
  readonly page: number;
  readonly pageSize: number;
  readonly total: number;
}

export interface TaskItem {
  readonly id: string;
  readonly type: 'scan' | 'transcode' | 'thumbnail' | 'preview' | 'metadata' | 'export' | 'ai';
  readonly status: TaskState;
  readonly priority: number;
  readonly progress: number;
  readonly spaceId: string;
  readonly error: string | null;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export interface ApiClientOptions {
  readonly authToken?: string;
  readonly baseUrl?: string;
  readonly fetch?: FetchLike;
  readonly retry?: RetryOptions;
  readonly space: SpaceContext;
  readonly timeoutMs?: number;
}

export interface RetryOptions {
  readonly attempts: number;
  readonly statuses?: readonly number[];
}

export interface ApiClient {
  readonly request: <T>(path: string, init?: RequestInit) => Promise<T>;
  readonly space: SpaceContext;
  readonly withSpace: (space: SpaceContext) => ApiClient;
}

export interface QueryKeyFactory {
  readonly mediaList: (
    space: SpaceContext,
    page: number | PageParams,
  ) => readonly ['media', 'list', string, number | PageParams];
  readonly mediaDetail: (space: SpaceContext, id: string) => readonly ['media', 'detail', string, string];
  readonly taskDetail: (space: SpaceContext, id: string) => readonly ['tasks', 'detail', string, string];
  readonly taskList: (space: SpaceContext) => readonly ['tasks', 'list', string];
}

export function createQueryKeys(): QueryKeyFactory {
  return {
    mediaList: (space, page) => ['media', 'list', space.spaceId, page] as const,
    mediaDetail: (space, id) => ['media', 'detail', space.spaceId, id] as const,
    taskDetail: (space, id) => ['tasks', 'detail', space.spaceId, id] as const,
    taskList: (space) => ['tasks', 'list', space.spaceId] as const,
  };
}

export function normalizeLegacyTaskState(state: string): TaskState {
  if (state === 'completed') {
    return 'succeeded';
  }
  if (state === 'error') {
    return 'failed';
  }
  if (state === 'pending' || state === 'running' || state === 'succeeded' || state === 'failed' || state === 'canceled') {
    return state;
  }
  throw new Error(`未知任务状态：${state}`);
}

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export function createApiClient(options: ApiClientOptions): ApiClient {
  const baseUrl = options.baseUrl ?? 'http://localhost';
  const fetchImpl = options.fetch ?? fetch;

  const request = async <T>(path: string, init: RequestInit = {}): Promise<T> => {
    const url = new URL(path, baseUrl);
    const attempts = Math.max(1, options.retry?.attempts ?? 1);
    let lastError: ApiError | undefined;
    for (let attempt = 1; attempt <= attempts; attempt += 1) {
      const result = await requestAttempt<T>(fetchImpl, url, options, init);
      if (result.ok) {
        return result.value;
      }
      lastError = result.error;
      if (!shouldRetry(result.error, attempt, attempts, options.retry)) {
        throw result.error;
      }
    }
    throw lastError ?? new ApiError(0, 'NETWORK_ERROR', '网络请求失败');
  };

  return {
    request,
    space: options.space,
    withSpace: (space) => createApiClient({ ...options, space }),
  };
}

export function detectDeviceCapabilities(input: DeviceDetectionInput = {}): DeviceCapabilities {
  const navigatorLike = input.navigator;
  const userAgent = navigatorLike?.userAgent?.toLowerCase() ?? '';
  const touch = (navigatorLike?.maxTouchPoints ?? 0) > 0 || input.matchMedia?.('(pointer: coarse)').matches === true;
  return {
    network: detectNetwork(navigatorLike),
    platform: detectPlatform(userAgent),
    pointer: touch ? 'coarse' : 'fine',
    touch,
  };
}

type RequestResult<T> = { readonly ok: true; readonly value: T } | { readonly error: ApiError; readonly ok: false };

interface DeviceDetectionInput {
  readonly matchMedia?: (query: string) => { readonly matches: boolean };
  readonly navigator?: {
    readonly connection?: {
      readonly downlink?: number;
      readonly effectiveType?: string;
      readonly saveData?: boolean;
    };
    readonly maxTouchPoints?: number;
    readonly onLine?: boolean;
    readonly userAgent?: string;
  };
}

async function requestAttempt<T>(
  fetchImpl: FetchLike,
  url: URL,
  options: ApiClientOptions,
  init: RequestInit,
): Promise<RequestResult<T>> {
  const timeoutMs = options.timeoutMs ?? 10_000;
  try {
    const controller = new AbortController();
    const timeout = setTimeout(() => {
      controller.abort();
    }, timeoutMs);
    try {
      return { ok: true, value: await requestJson<T>(fetchImpl, url, options, init, controller.signal) };
    } finally {
      clearTimeout(timeout);
    }
  } catch (error) {
    return {
      error: error instanceof ApiError ? error : new ApiError(0, 'NETWORK_ERROR', '网络请求失败'),
      ok: false,
    };
  }
}

export async function listMedia(client: ApiClient, params: PageParams): Promise<PageResult<MediaItem>> {
  const searchParams = new URLSearchParams({
    page: String(params.page),
    page_size: String(params.pageSize),
  });
  const response = await client.request<RawMediaPage>(`/api/v2/media?${searchParams.toString()}`);
  return {
    items: response.items.map(toMediaItem),
    page: response.page,
    pageSize: response.page_size,
    total: response.total,
  };
}

export async function getMedia(client: ApiClient, id: string): Promise<MediaItem> {
  return toMediaItem(await client.request<RawMediaItem>(`/api/v2/media/${encodeURIComponent(id)}`));
}

export async function getTask(client: ApiClient, id: string): Promise<TaskItem> {
  return toTaskItem(await client.request<RawTaskItem>(`/api/v2/tasks/${encodeURIComponent(id)}`));
}

export function taskPollInterval(task: TaskItem): 2000 | false {
  return task.status === 'pending' || task.status === 'running' ? 2_000 : false;
}

interface RawMediaPage {
  readonly items: readonly RawMediaItem[];
  readonly page: number;
  readonly page_size: number;
  readonly total: number;
}

interface RawMediaItem {
  readonly created_at: string;
  readonly duration_seconds: number;
  readonly id: string;
  readonly kind: 'video' | 'image';
  readonly space_id: string;
  readonly title: string;
}

interface RawTaskItem {
  readonly created_at: string;
  readonly error: string | null;
  readonly id: string;
  readonly priority: number;
  readonly progress: number;
  readonly space_id: string;
  readonly status: string;
  readonly type: TaskItem['type'];
  readonly updated_at: string;
}

function toMediaItem(item: RawMediaItem): MediaItem {
  return {
    createdAt: item.created_at,
    durationSeconds: item.duration_seconds,
    id: item.id,
    kind: item.kind,
    spaceId: item.space_id,
    title: item.title,
  };
}

function toTaskItem(item: RawTaskItem): TaskItem {
  return {
    createdAt: item.created_at,
    error: item.error,
    id: item.id,
    priority: item.priority,
    progress: item.progress,
    spaceId: item.space_id,
    status: normalizeLegacyTaskState(item.status),
    type: item.type,
    updatedAt: item.updated_at,
  };
}

async function requestJson<T>(
  fetchImpl: FetchLike,
  url: URL,
  options: ApiClientOptions,
  init: RequestInit,
  signal: AbortSignal,
): Promise<T> {
  const response = await fetchImpl(url, {
    ...init,
    headers: buildHeaders(init.headers, options),
    signal,
  });
  if (!response.ok) {
    throw await toApiError(response);
  }
  return (await response.json()) as T;
}

function buildHeaders(headers: HeadersInit | undefined, options: ApiClientOptions): Headers {
  const result = new Headers(headers);
  result.set('Accept', 'application/json');
  result.set('X-JianVideo-Space-Id', options.space.spaceId);
  if (options.authToken !== undefined) {
    result.set('Authorization', `Bearer ${options.authToken}`);
  }
  return result;
}

async function toApiError(response: Response): Promise<ApiError> {
  const body = (await response.json().catch(() => undefined)) as unknown;
  if (isErrorBody(body)) {
    return new ApiError(response.status, body.code, body.message);
  }
  return new ApiError(response.status, `HTTP_${String(response.status)}`, '接口请求失败');
}

function isErrorBody(value: unknown): value is { readonly code: string; readonly message: string } {
  return typeof value === 'object' && value !== null && 'code' in value && 'message' in value;
}

function shouldRetry(error: ApiError, attempt: number, attempts: number, retry: RetryOptions | undefined): boolean {
  if (attempt >= attempts) {
    return false;
  }
  if (retry?.statuses !== undefined) {
    return retry.statuses.includes(error.status);
  }
  return error.status === 0 || error.status === 408 || error.status >= 500;
}

function detectPlatform(userAgent: string): DevicePlatform {
  if (userAgent.includes('automotive')) {
    return 'automotive';
  }
  if (
    userAgent.includes('android tv') ||
    userAgent.includes('appletv') ||
    userAgent.includes('smart-tv') ||
    userAgent.includes('hbbtv') ||
    userAgent.includes('tizen') ||
    userAgent.includes('webos')
  ) {
    return 'tv';
  }
  if (
    userAgent.includes('mobile') ||
    userAgent.includes('android') ||
    userAgent.includes('iphone') ||
    userAgent.includes('ipad')
  ) {
    return 'mobile';
  }
  if (
    userAgent.includes('windows') ||
    userAgent.includes('macintosh') ||
    userAgent.includes('x11') ||
    userAgent.includes('linux')
  ) {
    return 'desktop';
  }
  return 'web';
}

function detectNetwork(navigatorLike: DeviceDetectionInput['navigator']): NetworkCapability {
  if (navigatorLike?.onLine === false) {
    return 'offline';
  }
  const connection = navigatorLike?.connection;
  if (connection?.saveData === true || connection?.effectiveType === 'slow-2g' || connection?.effectiveType === '2g') {
    return 'constrained';
  }
  if (connection?.effectiveType === '4g' || (connection?.downlink ?? 0) >= 10) {
    return 'fast';
  }
  return 'standard';
}
