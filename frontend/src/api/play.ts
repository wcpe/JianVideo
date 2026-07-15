import client from './client';
import type { ClientCapabilities } from '@/utils/codec-capability';
import type { PlaybackDescriptor } from '@/types';

/**
 * 端到端编码协商（FR-53）。
 *
 * 上报客户端高级编码能力，后端按「首选优先级 ∩ 客户端能力 ∩ 实测可产出」协商出
 * 实际输出编码与播放路径，返回播放描述符交自适应播放器分发。
 */

/** 后端协商响应（snake_case）；前端转为 PlaybackDescriptor。 */
export type HLSTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';

export interface HLSPreviewStatus {
  available: boolean;
  profile_id: string;
  url: string;
  effective_track_id?: string | null;
  task: { id: string; status: HLSTaskStatus; progress: number } | null;
}

export interface AudioReloadTaskResponse {
  task_id: string;
  profile_id: string;
  requested_track_id: string;
  space_id: string;
  url: string;
}

export interface TimelinePreviewStatus {
  duration: number;
  generation_id?: string;
  profile_id: string;
  source_fingerprint?: string;
  sprite_urls?: Readonly<Record<string, string>>;
  status: 'available' | 'pending';
  task_id?: number;
  version: number;
  vtt_url?: string;
}

export interface HLSABRTaskResponse {
  task_id: number;
  profile_id: 'abr-h264';
  url: string;
}

export interface HLSABRTaskInput {
  priority?: number;
  force_rebuild?: boolean;
}

interface NegotiateResponse {
  codec: string;
  path: 'ts' | 'fmp4' | 'mp4';
  url: string;
  mime?: string;
  fallback_url?: string;
}

/**
 * 请求后端协商，返回播放描述符。
 * 后端 url 为相对路径，此处绝对化以兼容 mpegts.js/hls.js 在 Web Worker 中的 fetch。
 */
export async function getHLSStatus(
  mediaID: number,
  profileID = 'h264',
  taskIDOrSignal?: string | AbortSignal,
  signal?: AbortSignal,
): Promise<HLSPreviewStatus> {
  const taskID = typeof taskIDOrSignal === 'string' ? taskIDOrSignal : undefined;
  const requestSignal = typeof taskIDOrSignal === 'string' ? signal : taskIDOrSignal;
  const res = await client.get<HLSPreviewStatus>(`/api/play/${mediaID}/hls-status`, {
    params: { profile_id: profileID, ...(taskID ? { task_id: taskID } : {}) },
    signal: requestSignal,
  });
  return { ...res.data, url: new URL(res.data.url, window.location.href).toString() };
}

export async function createAudioReload(
  mediaID: number,
  trackID: string,
  signal?: AbortSignal,
): Promise<AudioReloadTaskResponse> {
  const response = await client.post<AudioReloadTaskResponse>(
    `/api/play/${mediaID}/audio-reload`,
    { track_id: trackID },
    { signal },
  );
  return { ...response.data, url: new URL(response.data.url, window.location.href).toString() };
}

export async function createHLSABR(
  mediaID: number,
  input: HLSABRTaskInput = {},
): Promise<HLSABRTaskResponse> {
  const res = await client.post<HLSABRTaskResponse>(`/api/play/${mediaID}/hls-abr`, input);
  return { ...res.data, url: new URL(res.data.url, window.location.href).toString() };
}

export async function getTimelinePreviewStatus(
  mediaID: number,
  profileID?: string,
  signal?: AbortSignal,
): Promise<TimelinePreviewStatus> {
  const params = profileID ? { profile: profileID } : undefined;
  const response = await client.get<TimelinePreviewStatus>(`/api/play/${mediaID}/timeline-preview`, {
    params,
    signal,
  });
  return absoluteTimelinePreviewUrls(response.data);
}

export async function getTimelinePreview(
  mediaID: number,
  profileID?: string,
  signal?: AbortSignal,
): Promise<TimelinePreviewStatus> {
  return getTimelinePreviewStatus(mediaID, profileID, signal);
}

export async function rebuildTimelinePreview(
  mediaID: number,
  profileID?: string,
  signal?: AbortSignal,
): Promise<TimelinePreviewStatus> {
  const body = profileID ? { profile_id: profileID } : {};
  const response = await client.post<TimelinePreviewStatus>(
    `/api/play/${mediaID}/timeline-preview/rebuild`,
    body,
    { signal },
  );
  return absoluteTimelinePreviewUrls(response.data);
}

function absoluteTimelinePreviewUrls(status: TimelinePreviewStatus): TimelinePreviewStatus {
  const absolute = (value: string) => new URL(value, window.location.href).toString();
  const spriteUrls = status.sprite_urls === undefined
    ? undefined
    : Object.fromEntries(Object.entries(status.sprite_urls).map(([name, url]) => [name, absolute(url)]));
  return {
    ...status,
    ...(spriteUrls === undefined ? {} : { sprite_urls: spriteUrls }),
    ...(status.vtt_url === undefined ? {} : { vtt_url: absolute(status.vtt_url) }),
  };
}

export async function negotiate(
  mediaID: number,
  caps: ClientCapabilities,
  signal?: AbortSignal,
): Promise<PlaybackDescriptor> {
  const res = await client.post<NegotiateResponse>(
    `/api/play/${mediaID}/negotiate`,
    { client_caps: caps },
    { signal },
  );
  const data = res.data;
  const toAbsolute = (path: string) => new URL(path, window.location.href).toString();
  return {
    codec: data.codec,
    path: data.path,
    url: toAbsolute(data.url),
    fallbackUrl: data.fallback_url ? toAbsolute(data.fallback_url) : undefined,
  };
}
