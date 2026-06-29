import type { SubtitleTrack } from '@/types';

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true';

import client from './client';

// ─── 真实 API 实现 ──────────────────────────────────

export async function realGetSubtitles(mediaId: number): Promise<SubtitleTrack[]> {
  const res = await client.get<{ tracks: SubtitleTrack[] }>(`/api/play/${mediaId}/subtitles`);
  return res.data.tracks;
}

export async function realGetSubtitleContent(mediaId: number, index: number): Promise<string> {
  const res = await client.get<string>(`/api/play/${mediaId}/subtitles/${index}`, {
    responseType: 'text',
  });
  return res.data;
}

// ─── Mock API 实现 ──────────────────────────────────

const mockTracks: SubtitleTrack[] = [
  { index: 0, file_name: '电影名.srt', format: 'srt', url: '/api/play/1/subtitles/0' },
  { index: 1, file_name: '电影名.ass', format: 'ass', url: '/api/play/1/subtitles/1' },
];

const mockVttContent = `WEBVTT

1
00:00:01.000 --> 00:00:03.000
这是第一条测试字幕

2
00:00:04.000 --> 00:00:06.000
这是第二条测试字幕
`;

async function mockGetSubtitles(_mediaId: number): Promise<SubtitleTrack[]> {
  await mockDelay(100);
  return [...mockTracks];
}

async function mockGetSubtitleContent(_mediaId: number, _index: number): Promise<string> {
  await mockDelay(50);
  return mockVttContent;
}

function mockDelay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function getSubtitles(mediaId: number) {
  return useMock ? mockGetSubtitles(mediaId) : realGetSubtitles(mediaId);
}
export function getSubtitleContent(mediaId: number, index: number) {
  return useMock ? mockGetSubtitleContent(mediaId, index) : realGetSubtitleContent(mediaId, index);
}
