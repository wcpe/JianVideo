import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import { getContinueWatching, getContinueWatchingStates, getWatchHistory } from './library';

describe('观看历史与继续观看 API', () => {
  it('解析稳定游标并保留媒体与观看状态绑定', async () => {
    let cursor = '';
    let limit = '';
    server.use(
      http.get('*/api/library/watch-history', ({ request }) => {
        const params = new URL(request.url).searchParams;
        cursor = params.get('cursor') ?? '';
        limit = params.get('limit') ?? '';
        return HttpResponse.json({
          items: [watchMediaItem()],
          next_cursor: 'cursor-next',
        });
      }),
    );

    const page = await getWatchHistory({ cursor: 'cursor-old', limit: 5 });

    expect(cursor).toBe('cursor-old');
    expect(limit).toBe('5');
    expect(page).toMatchObject({
      items: [{ media: { id: 9 }, watch_state: { position_seconds: 42, revision: 3 } }],
      next_cursor: 'cursor-next',
    });
  });

  it('继续观看读取真源状态，同时给旧 UI 投影兼容字段', async () => {
    server.use(
      http.get('*/api/library/continue-watching', () =>
        HttpResponse.json({
          items: [watchMediaItem()],
        }),
      ),
    );

    const states = await getContinueWatchingStates(8);
    const legacy = await getContinueWatching(8);

    expect(states[0]).toMatchObject({
      media: { id: 9 },
      watch_state: { completed: false, position_seconds: 42, revision: 3 },
    });
    expect(legacy[0]).toMatchObject({
      id: 9,
      last_position: 42,
      last_watched_at: '2026-07-15T10:00:00Z',
      watched: false,
    });
  });
});

function watchMediaItem() {
  return {
    media: {
      added_at: '2026-07-01T00:00:00Z',
      audio_codec: 'aac',
      bitrate: 1000,
      duration: 100,
      file_name: 'demo.mp4',
      file_path: 'D:/Videos/demo.mp4',
      file_size: 100,
      format: 'mp4',
      height: 1080,
      id: 9,
      library_id: 1,
      modified_at: '2026-07-01T00:00:00Z',
      space_id: 'space-default',
      subtitle_tracks: '',
      video_codec: 'h264',
      width: 1920,
    },
    watch_state: {
      completed: false,
      completed_at: null,
      created_at: '2026-07-15T09:00:00Z',
      last_event_seq: 5,
      last_session_id: 'session-a',
      last_watched_at: '2026-07-15T10:00:00Z',
      media_id: 9,
      position_seconds: 42,
      revision: 3,
      space_id: 'space-default',
      updated_at: '2026-07-15T10:00:00Z',
    },
  };
}
