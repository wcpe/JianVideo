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
export interface HLSPreviewStatus {
  available: boolean;
  profile_id: string;
  url: string;
  task: { id: string; status: string; progress: number } | null;
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
export async function getHLSStatus(mediaID: number, profileID = 'h264'): Promise<HLSPreviewStatus> {
  const res = await client.get<HLSPreviewStatus>(`/api/play/${mediaID}/hls-status`, {
    params: { profile_id: profileID },
  });
  return { ...res.data, url: new URL(res.data.url, window.location.href).toString() };
}

export async function negotiate(
  mediaID: number,
  caps: ClientCapabilities,
): Promise<PlaybackDescriptor> {
  const res = await client.post<NegotiateResponse>(`/api/play/${mediaID}/negotiate`, {
    client_caps: caps,
  });
  const data = res.data;
  const toAbsolute = (path: string) => new URL(path, window.location.href).toString();
  return {
    codec: data.codec,
    path: data.path,
    url: toAbsolute(data.url),
    fallbackUrl: data.fallback_url ? toAbsolute(data.fallback_url) : undefined,
  };
}
