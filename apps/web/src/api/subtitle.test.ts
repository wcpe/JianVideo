import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import {
  deleteSubtitle,
  getSubtitleContent,
  getTrackContent,
  getTracks,
  uploadSubtitle,
} from './subtitle';

const response = {
  tracks: [
    {
      id: 'uploaded:track 1',
      kind: 'subtitle',
      label: '中文字幕',
      source: 'uploaded',
      format: 'vtt',
      language: 'zh',
      is_default: true,
      is_forced: false,
      available: true,
      capability: 'seamless',
      stream_index: 2,
    },
    {
      id: 'audio-a',
      kind: 'audio',
      label: '中文音轨',
      source: 'embedded',
      available: true,
      capability: 'unsupported',
    },
  ],
  selection: {
    audio: { selected_track_id: 'audio-a', effective_track_id: null },
    subtitle: { selected_track_id: 'uploaded:track 1', effective_track_id: null },
  },
  sources: {
    sidecar: {
      available: false,
      capability: 'unsupported',
      unsupported_reason: 'SMB_SIDECAR_UNSUPPORTED',
    },
  },
  backend: { audio: { available: false, capability: 'unsupported' } },
};

describe('统一轨道 API', () => {
  it('映射 snake_case DTO 并保持字符串轨道 ID', async () => {
    server.use(http.get('*/api/play/9/tracks', () => HttpResponse.json(response)));

    const result = await getTracks(9);

    expect(result.tracks[0]).toMatchObject({
      id: 'uploaded:track 1',
      streamIndex: 2,
      default: true,
      forced: false,
    });
    expect(result.selection.audio).toEqual({
      selectedTrackId: 'audio-a',
      effectiveTrackId: null,
    });
    expect(result.selection.subtitle).toEqual({
      selectedTrackId: 'uploaded:track 1',
      effectiveTrackId: null,
    });
    expect(result.sources.sidecar.unsupportedReason).toBe('SMB_SIDECAR_UNSUPPORTED');
  });

  it('使用稳定轨道 ID 请求内容与删除', async () => {
    const urls: string[] = [];
    server.use(
      http.get('*/api/play/9/subtitles/:trackId/content', ({ request }) => {
        urls.push(new URL(request.url).pathname);
        return new HttpResponse('WEBVTT', { headers: { 'content-type': 'text/vtt' } });
      }),
      http.delete('*/api/play/9/subtitles/:trackId', ({ request }) => {
        urls.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(getTrackContent(9, 'uploaded:track 1')).resolves.toBe('WEBVTT');
    await deleteSubtitle(9, 'uploaded:track 1');

    expect(urls).toEqual([
      '/api/play/9/subtitles/uploaded%3Atrack%201/content',
      '/api/play/9/subtitles/uploaded%3Atrack%201',
    ]);
  });

  it('上传使用 file 字段的 FormData', async () => {
    const upload = { form: null as FormData | null };
    server.use(
      http.post('*/api/play/9/subtitles', async ({ request }) => {
        upload.form = await request.formData();
        return HttpResponse.json(response.tracks[0], { status: 201 });
      }),
    );
    const file = new File(['WEBVTT'], 'sample.vtt', { type: 'text/vtt' });

    const result = await uploadSubtitle(9, file);

    expect(upload.form?.has('file')).toBe(true);
    expect(result.id).toBe('uploaded:track 1');
  });

  it('旧内容导出仍接受 index，但先解析为稳定 ID', async () => {
    server.use(
      http.get('*/api/play/9/tracks', () => HttpResponse.json(response)),
      http.get(
        '*/api/play/9/subtitles/:trackId/content',
        () => new HttpResponse('WEBVTT\n\n', { headers: { 'content-type': 'text/vtt' } }),
      ),
    );

    await expect(getSubtitleContent(9, 0)).resolves.toBe('WEBVTT\n\n');
  });
});
