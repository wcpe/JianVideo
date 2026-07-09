import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import {
  addMediaExtension,
  createMediaTypeRule,
  deleteMediaTypeRule,
  listMediaTypes,
  updateMediaTypeRule,
} from './library';

describe('media-types api client', () => {
  it('按库查询媒体类型与扫描规则', async () => {
    let libraryID = '';
    server.use(
      http.get('*/api/media-types', ({ request }) => {
        libraryID = new URL(request.url).searchParams.get('library_id') || '';
        return HttpResponse.json({
          types: [
            {
              type: 'video',
              name: '视频',
              description: '可播放、可转码的视频文件。',
              default_extensions: ['mp4'],
              capabilities: ['scan', 'transcode'],
            },
          ],
          rules: [
            {
              id: 'builtin-video-mp4',
              space_id: 'space-default',
              library_id: 7,
              type: 'video',
              extension: 'mp4',
              label: 'MP4 视频',
              description: '常见视频容器。',
              enabled: true,
              builtin: true,
              capabilities: ['scan', 'transcode'],
            },
          ],
        });
      }),
    );

    const data = await listMediaTypes(7);

    expect(libraryID).toBe('7');
    expect(data.types[0].name).toBe('视频');
    expect(data.rules[0].label).toBe('MP4 视频');
  });

  it('新增媒体类型规则时保留库 ID 和原始后缀输入', async () => {
    let payload: unknown = null;
    server.use(
      http.post('*/api/media-types/rules', async ({ request }) => {
        payload = await request.json();
        return HttpResponse.json(
          {
            id: 9,
            space_id: 'space-default',
            library_id: 7,
            type: 'image',
            extension: 'rawx',
            label: '',
            description: '',
            enabled: true,
            builtin: false,
            capabilities: ['scan', 'thumbnail'],
          },
          { status: 201 },
        );
      }),
    );

    await createMediaTypeRule({ library_id: 7, type: 'image', extension: '.rawx' });

    expect(payload).toEqual({ library_id: 7, type: 'image', extension: '.rawx' });
  });

  it('更新和删除媒体类型规则走新规则端点', async () => {
    const calls: string[] = [];
    let putPayload: unknown = null;
    server.use(
      http.put('*/api/media-types/rules/:id', async ({ params, request }) => {
        calls.push(`put:${String(params.id)}`);
        putPayload = await request.json();
        return HttpResponse.json({
          id: params.id,
          space_id: 'space-default',
          library_id: 7,
          type: 'video',
          extension: 'mp4',
          label: 'MP4 视频',
          description: '常见视频容器。',
          enabled: false,
          builtin: true,
          capabilities: ['scan', 'transcode'],
        });
      }),
      http.delete('*/api/media-types/rules/:id', ({ params }) => {
        calls.push(`delete:${String(params.id)}`);
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await updateMediaTypeRule('builtin-video-mp4', { enabled: false });
    await deleteMediaTypeRule(9);

    expect(putPayload).toEqual({ enabled: false });
    expect(calls).toEqual(['put:builtin-video-mp4', 'delete:9']);
  });

  it('保留旧媒体库后缀添加接口', async () => {
    let payload: unknown = null;
    server.use(
      http.post('*/api/library/extensions', async ({ request }) => {
        payload = await request.json();
        return new HttpResponse(null, { status: 201 });
      }),
    );

    await addMediaExtension(3, '.foo', 'video');

    expect(payload).toEqual({ library_id: 3, extension: '.foo', type: 'video' });
  });
});
