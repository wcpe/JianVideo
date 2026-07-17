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

export type WatchEventType = 'progress' | 'pause' | 'seek' | 'ended';
export type WatchEventReason = 'user' | 'ab_loop' | 'restore' | 'system';

export interface WatchState {
  readonly completed: boolean;
  readonly completedAt: string | null;
  readonly createdAt: string;
  readonly eventSeq: number;
  readonly lastWatchedAt: string;
  readonly mediaId: string;
  readonly positionSeconds: number;
  readonly revision: number;
  readonly sessionId: string;
  readonly spaceId: string;
  readonly updatedAt: string;
}

export interface WatchStateEvent {
  readonly durationSeconds?: number;
  readonly eventSeq: number;
  readonly eventType: WatchEventType;
  readonly expectedRevision: number;
  readonly positionSeconds: number;
  readonly reason: WatchEventReason;
  readonly sessionId: string;
}

export interface WatchStateUpdateResult {
  readonly applied: boolean;
  readonly current: WatchState;
}

export interface WatchMediaEntry<TMedia = unknown> {
  readonly media: TMedia;
  readonly watchState: WatchState;
}

export interface WatchHistoryParams {
  readonly cursor?: string;
  readonly limit?: number;
}

export interface WatchHistoryPage<TMedia = unknown> {
  readonly items: readonly WatchMediaEntry<TMedia>[];
  readonly nextCursor?: string;
}

export interface PageResult<T> {
  readonly items: readonly T[];
  readonly page: number;
  readonly pageSize: number;
  readonly total: number;
}

export type MediaIdentifier = number | string;

export interface MediaChapter {
  readonly endMs: number;
  readonly id: string;
  readonly language: string;
  readonly source: 'embedded';
  readonly sourceIndex: number;
  readonly startMs: number;
  readonly title: string;
}

export interface MediaChaptersResult {
  readonly items: readonly MediaChapter[];
  readonly parsedAt: string | null;
  readonly stale: boolean;
}

export interface MediaBookmark {
  readonly createdAt: string;
  readonly id: string;
  readonly note: string | null;
  readonly positionMs: number;
  readonly revision: number;
  readonly title: string;
  readonly updatedAt: string;
}

export interface MediaBookmarkInput {
  readonly note?: string | null;
  readonly positionMs: number;
  readonly title: string;
}

export interface MediaBookmarkUpdate {
  readonly note: string | null;
  readonly positionMs: number;
  readonly revision: number;
  readonly title: string;
}

export interface BookmarkMutationOptions {
  readonly reload: () => Promise<void>;
  readonly onReloadError?: (error: unknown) => Promise<void> | void;
  readonly reloadAfterSuccess?: boolean;
}

export type TaskType = string;

export interface TaskItem {
  readonly id: string;
  readonly type: TaskType;
  readonly status: TaskState;
  readonly priority: number;
  readonly progress: number;
  readonly spaceId: string | null;
  readonly error: string | null;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface TaskListParams {
  readonly page?: number;
  readonly pageSize?: number;
  readonly resourceId?: string;
  readonly resourceType?: string;
  readonly status?: TaskState;
  readonly type?: TaskType;
}

export interface TaskStats {
  readonly byStatus: Partial<Record<TaskState, number>>;
  readonly byType: Record<string, number>;
  readonly total: number;
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
  readonly mediaChapters: (
    space: SpaceContext,
    mediaId: MediaIdentifier,
  ) => readonly ['media', 'chapters', string, string];
  readonly mediaBookmarks: (
    space: SpaceContext,
    mediaId: MediaIdentifier,
  ) => readonly ['media', 'bookmarks', string, string];
  readonly taskDetail: (space: SpaceContext, id: string) => readonly ['tasks', 'detail', string, string];
  readonly taskList: (space: SpaceContext) => readonly ['tasks', 'list', string];
}

export function createQueryKeys(): QueryKeyFactory {
  return {
    mediaList: (space, page) => ['media', 'list', space.spaceId, page] as const,
    mediaDetail: (space, id) => ['media', 'detail', space.spaceId, id] as const,
    mediaChapters: (space, mediaId) => ['media', 'chapters', space.spaceId, String(mediaId)] as const,
    mediaBookmarks: (space, mediaId) => ['media', 'bookmarks', space.spaceId, String(mediaId)] as const,
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

export class WatchStateConflictError extends ApiError {
  readonly current: WatchState;

  constructor(message: string, current: WatchState) {
    super(409, 'WATCH_STATE_CONFLICT', message);
    this.name = 'WatchStateConflictError';
    this.current = current;
  }
}

export class BookmarkConflictError extends ApiError {
  readonly current: MediaBookmark | null;
  readonly deleted: boolean;

  constructor(message: string, current: MediaBookmark | null, deleted: boolean) {
    super(409, 'BOOKMARK_CONFLICT', message);
    this.name = 'BookmarkConflictError';
    this.current = current;
    this.deleted = deleted;
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

function mergeAbortSignals(primary: AbortSignal, secondary?: AbortSignal | null): AbortSignal {
  if (!secondary) return primary;
  const controller = new AbortController();
  const abort = () => {
    controller.abort();
  };
  if (primary.aborted || secondary.aborted) abort();
  else {
    primary.addEventListener('abort', abort, { once: true });
    secondary.addEventListener('abort', abort, { once: true });
  }
  return controller.signal;
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
      const signal = mergeAbortSignals(controller.signal, init.signal);
      return { ok: true, value: await requestJson<T>(fetchImpl, url, options, init, signal) };
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

export async function getWatchState(
  client: ApiClient,
  mediaId: string,
  options: Pick<RequestInit, 'signal'> = {},
): Promise<WatchState> {
  const response = await client.request<RawWatchState>(watchStatePath(mediaId), options);
  return toWatchState(response);
}

export async function updateWatchState(
  client: ApiClient,
  mediaId: string,
  event: WatchStateEvent,
  options: Pick<RequestInit, 'keepalive' | 'signal'> = {},
): Promise<WatchStateUpdateResult> {
  const response = await client.request<RawWatchStateUpdateResult>(watchStatePath(mediaId), {
    ...options,
    body: JSON.stringify(toRawWatchStateEvent(event)),
    headers: { 'Content-Type': 'application/json' },
    method: 'PUT',
  });
  return { applied: response.applied, current: toWatchState(response.current) };
}

export async function listWatchHistory<TMedia = unknown>(
  client: ApiClient,
  params: WatchHistoryParams = {},
): Promise<WatchHistoryPage<TMedia>> {
  const response = await client.request<RawWatchHistoryPage<TMedia>>(
    buildPath('/api/library/watch-history', watchListSearchParams(params)),
  );
  return {
    items: response.items.map(toWatchMediaEntry),
    ...(response.next_cursor === undefined ? {} : { nextCursor: response.next_cursor }),
  };
}

export async function listContinueWatching<TMedia = unknown>(
  client: ApiClient,
  limit?: number,
): Promise<readonly WatchMediaEntry<TMedia>[]> {
  const params: WatchHistoryParams = limit === undefined ? {} : { limit };
  const response = await client.request<RawWatchList<TMedia>>(
    buildPath('/api/library/continue-watching', watchListSearchParams(params)),
  );
  return response.items.map(toWatchMediaEntry);
}

export async function getMediaChapters(
  client: ApiClient,
  mediaId: MediaIdentifier,
): Promise<MediaChaptersResult> {
  const response = await client.request<RawMediaChaptersResult>(`${mediaLibraryPath(mediaId)}/chapters`);
  return {
    items: response.items.map(toMediaChapter),
    parsedAt: response.parsed_at,
    stale: response.stale,
  };
}

export async function listMediaBookmarks(
  client: ApiClient,
  mediaId: MediaIdentifier,
): Promise<readonly MediaBookmark[]> {
  const response = await client.request<RawMediaBookmarksResult>(`${mediaLibraryPath(mediaId)}/bookmarks`);
  return response.items.map(toMediaBookmark);
}

export async function createMediaBookmark(
  client: ApiClient,
  mediaId: MediaIdentifier,
  input: MediaBookmarkInput,
  options: BookmarkMutationOptions,
): Promise<MediaBookmark> {
  return mutateBookmark(
    () =>
      client
        .request<RawMediaBookmark>(`${mediaLibraryPath(mediaId)}/bookmarks`, jsonRequest('POST', bookmarkBody(input)))
        .then(toMediaBookmark),
    options,
  );
}

export async function updateMediaBookmark(
  client: ApiClient,
  mediaId: MediaIdentifier,
  bookmarkId: string,
  input: MediaBookmarkUpdate,
  options: BookmarkMutationOptions,
): Promise<MediaBookmark> {
  const path = `${mediaLibraryPath(mediaId)}/bookmarks/${encodeURIComponent(bookmarkId)}`;
  return mutateBookmark(
    () => client.request<RawMediaBookmark>(path, jsonRequest('PUT', bookmarkBody(input))).then(toMediaBookmark),
    options,
  );
}

export async function deleteMediaBookmark(
  client: ApiClient,
  mediaId: MediaIdentifier,
  bookmarkId: string,
  revision: number,
  options: BookmarkMutationOptions,
): Promise<void> {
  const query = new URLSearchParams({ revision: String(revision) });
  const path = `${mediaLibraryPath(mediaId)}/bookmarks/${encodeURIComponent(bookmarkId)}?${query.toString()}`;
  return mutateBookmark(() => client.request<undefined>(path, { method: 'DELETE' }), options);
}

export async function listTasks(client: ApiClient, params: TaskListParams = {}): Promise<PageResult<TaskItem>> {
  const response = await client.request<RawTaskPage>(buildPath('/api/tasks', taskSearchParams(params)));
  return {
    items: response.items.map(toTaskItem),
    page: response.page,
    pageSize: response.page_size,
    total: response.total,
  };
}

export async function getTask(client: ApiClient, id: string): Promise<TaskItem> {
  return toTaskItem(await client.request<RawTaskItem>(`/api/tasks/${encodeURIComponent(id)}`));
}

export async function getTaskStats(client: ApiClient, params: Pick<TaskListParams, 'status' | 'type'> = {}): Promise<TaskStats> {
  const response = await client.request<RawTaskStats>(buildPath('/api/tasks/stats', taskSearchParams(params)));
  return {
    byStatus: normalizeTaskStatusCounts(response.by_status),
    byType: response.by_type,
    total: response.total,
  };
}

export async function cancelTask(client: ApiClient, id: string): Promise<TaskItem> {
  return toTaskItem(await client.request<RawTaskItem>(`/api/tasks/${encodeURIComponent(id)}/cancel`, { method: 'POST' }));
}

export async function retryTask(client: ApiClient, id: string): Promise<TaskItem> {
  return toTaskItem(await client.request<RawTaskItem>(`/api/tasks/${encodeURIComponent(id)}/retry`, { method: 'POST' }));
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

interface RawWatchState {
  readonly completed: boolean;
  readonly completed_at?: string | null;
  readonly created_at: string;
  readonly last_event_seq: number;
  readonly last_session_id: string;
  readonly last_watched_at: string;
  readonly media_id: number | string;
  readonly position_seconds: number;
  readonly revision: number;
  readonly space_id: string;
  readonly updated_at: string;
}

interface RawWatchStateUpdateResult {
  readonly applied: boolean;
  readonly current: RawWatchState;
}

interface RawWatchMediaEntry<TMedia> {
  readonly media: TMedia;
  readonly watch_state: RawWatchState;
}

interface RawWatchList<TMedia> {
  readonly items: readonly RawWatchMediaEntry<TMedia>[];
}

interface RawWatchHistoryPage<TMedia> extends RawWatchList<TMedia> {
  readonly next_cursor?: string;
}

interface RawWatchStateConflictBody {
  readonly code: 'WATCH_STATE_CONFLICT';
  readonly current: RawWatchState;
  readonly message: string;
}

interface RawMediaChapter {
  readonly end_ms: number;
  readonly id: string;
  readonly language?: string;
  readonly source: 'embedded';
  readonly source_index: number;
  readonly start_ms: number;
  readonly title: string;
}

interface RawMediaChaptersResult {
  readonly items: readonly RawMediaChapter[];
  readonly parsed_at: string | null;
  readonly stale: boolean;
}

interface RawMediaBookmark {
  readonly created_at: string;
  readonly id: string;
  readonly note: string | null;
  readonly position_ms: number;
  readonly revision: number;
  readonly title: string;
  readonly updated_at: string;
}

interface RawMediaBookmarksResult {
  readonly items: readonly RawMediaBookmark[];
}

interface RawTaskItem {
  readonly created_at: string;
  readonly error: string | null;
  readonly id: string;
  readonly priority: number;
  readonly progress: number;
  readonly space_id: string | null;
  readonly status: string;
  readonly type: TaskItem['type'];
  readonly updated_at: string;
}

interface RawTaskPage {
  readonly items: readonly RawTaskItem[];
  readonly page: number;
  readonly page_size: number;
  readonly total: number;
}

interface RawTaskStats {
  readonly by_status: Record<string, number>;
  readonly by_type: Record<string, number>;
  readonly total: number;
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

function toWatchState(state: RawWatchState): WatchState {
  return {
    completed: state.completed,
    completedAt: state.completed_at ?? null,
    createdAt: state.created_at,
    eventSeq: state.last_event_seq,
    lastWatchedAt: state.last_watched_at,
    mediaId: String(state.media_id),
    positionSeconds: state.position_seconds,
    revision: state.revision,
    sessionId: state.last_session_id,
    spaceId: state.space_id,
    updatedAt: state.updated_at,
  };
}

function toRawWatchStateEvent(event: WatchStateEvent): Record<string, unknown> {
  return {
    position_seconds: event.positionSeconds,
    expected_revision: event.expectedRevision,
    session_id: event.sessionId,
    event_seq: event.eventSeq,
    event_type: event.eventType,
    reason: event.reason,
    ...(event.durationSeconds === undefined ? {} : { duration_seconds: event.durationSeconds }),
  };
}

function toWatchMediaEntry<TMedia>(entry: RawWatchMediaEntry<TMedia>): WatchMediaEntry<TMedia> {
  return { media: entry.media, watchState: toWatchState(entry.watch_state) };
}

function toMediaChapter(item: RawMediaChapter): MediaChapter {
  return {
    endMs: item.end_ms,
    id: item.id,
    language: item.language ?? '',
    source: item.source,
    sourceIndex: item.source_index,
    startMs: item.start_ms,
    title: item.title,
  };
}

function toMediaBookmark(item: RawMediaBookmark): MediaBookmark {
  return {
    createdAt: item.created_at,
    id: item.id,
    note: item.note,
    positionMs: item.position_ms,
    revision: item.revision,
    title: item.title,
    updatedAt: item.updated_at,
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

function watchStatePath(mediaId: string): string {
  return `/api/play/${encodeURIComponent(mediaId)}/watch-state`;
}

function watchListSearchParams(params: WatchHistoryParams): URLSearchParams {
  const searchParams = new URLSearchParams();
  if (params.cursor !== undefined) {
    searchParams.set('cursor', params.cursor);
  }
  if (params.limit !== undefined) {
    searchParams.set('limit', String(params.limit));
  }
  return searchParams;
}

function mediaLibraryPath(mediaId: MediaIdentifier): string {
  return `/api/library/media/${encodeURIComponent(String(mediaId))}`;
}

function bookmarkBody(input: MediaBookmarkInput | MediaBookmarkUpdate): Record<string, number | string | null> {
  const body: Record<string, number | string | null> = {
    position_ms: input.positionMs,
    title: input.title,
  };
  if (input.note !== undefined) {
    body.note = input.note;
  }
  if ('revision' in input) {
    body.revision = input.revision;
  }
  return body;
}

function jsonRequest(method: 'POST' | 'PUT', body: unknown): RequestInit {
  return {
    body: JSON.stringify(body),
    headers: { 'Content-Type': 'application/json' },
    method,
  };
}

async function reloadAfterBookmarkMutation(options: BookmarkMutationOptions): Promise<void> {
  try {
    await options.reload();
  } catch (error) {
    try {
      await options.onReloadError?.(error);
    } catch {
      // 刷新错误回调不得把已经成功的服务端写入改判为失败。
    }
  }
}

async function mutateBookmark<T>(mutation: () => Promise<T>, options: BookmarkMutationOptions): Promise<T> {
  let result: T;
  try {
    result = await mutation();
  } catch (error) {
    if (error instanceof BookmarkConflictError) await reloadAfterBookmarkMutation(options);
    throw error;
  }
  if (options.reloadAfterSuccess !== false) await reloadAfterBookmarkMutation(options);
  return result;
}

function buildPath(path: string, params: URLSearchParams): string {
  const query = params.toString();
  return query ? `${path}?${query}` : path;
}

function taskSearchParams(params: Pick<TaskListParams, 'page' | 'pageSize' | 'resourceId' | 'resourceType' | 'status' | 'type'>): URLSearchParams {
  const searchParams = new URLSearchParams();
  if (params.page !== undefined) {
    searchParams.set('page', String(params.page));
  }
  if (params.pageSize !== undefined) {
    searchParams.set('page_size', String(params.pageSize));
  }
  if (params.type !== undefined) {
    searchParams.set('type', params.type);
  }
  if (params.status !== undefined) {
    searchParams.set('status', params.status);
  }
  if (params.resourceType !== undefined) {
    searchParams.set('resource_type', params.resourceType);
  }
  if (params.resourceId !== undefined) {
    searchParams.set('resource_id', params.resourceId);
  }
  return searchParams;
}

function normalizeTaskStatusCounts(counts: Record<string, number>): Partial<Record<TaskState, number>> {
  const result: Partial<Record<TaskState, number>> = {};
  for (const [status, count] of Object.entries(counts)) {
    const normalized = normalizeLegacyTaskState(status);
    result[normalized] = (result[normalized] ?? 0) + count;
  }
  return result;
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
  if (response.status === 204) {
    return undefined as T;
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
  if (response.status === 409 && isWatchStateConflictBody(body)) {
    return new WatchStateConflictError(body.message, toWatchState(body.current));
  }
  if (response.status === 409 && isBookmarkConflictBody(body)) {
    return new BookmarkConflictError(
      body.message,
      body.current === null ? null : toMediaBookmark(body.current),
      body.deleted,
    );
  }
  if (isErrorBody(body)) {
    return new ApiError(response.status, body.code, body.message);
  }
  return new ApiError(response.status, `HTTP_${String(response.status)}`, '接口请求失败');
}

function isWatchStateConflictBody(value: unknown): value is RawWatchStateConflictBody {
  return isErrorBody(value) && value.code === 'WATCH_STATE_CONFLICT' && 'current' in value;
}

function isBookmarkConflictBody(
  value: unknown,
): value is {
  readonly code: 'BOOKMARK_CONFLICT';
  readonly current: RawMediaBookmark | null;
  readonly deleted: boolean;
  readonly message: string;
} {
  return (
    isErrorBody(value) &&
    value.code === 'BOOKMARK_CONFLICT' &&
    'current' in value &&
    (value.current === null || isRawMediaBookmark(value.current)) &&
    'deleted' in value &&
    typeof value.deleted === 'boolean'
  );
}

function isRawMediaBookmark(value: unknown): value is RawMediaBookmark {
  return (
    typeof value === 'object' &&
    value !== null &&
    'created_at' in value &&
    'id' in value &&
    'note' in value &&
    'position_ms' in value &&
    'revision' in value &&
    'title' in value &&
    'updated_at' in value
  );
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
