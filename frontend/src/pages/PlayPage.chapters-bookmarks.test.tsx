import { act, render, screen, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  BookmarkConflictError,
  type BookmarkMutationOptions,
  type MediaBookmarkUpdate,
} from '../../../packages/media-client/src/index';
import { server } from '@/mocks/beforeAll';
import PlayPage from './PlayPage';

const { mediaClient, playerState } = vi.hoisted(() => ({
  mediaClient: {
    createMediaBookmark: vi.fn(),
    deleteMediaBookmark: vi.fn(),
    getMediaChapters: vi.fn(),
    listMediaBookmarks: vi.fn(),
    updateMediaBookmark: vi.fn(),
  },
  playerState: { props: {} as Record<string, unknown> },
}));

vi.mock('../../../packages/media-client/src/index', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../packages/media-client/src/index')>()),
  ...mediaClient,
}));

vi.mock('@/components/VideoPlayer', () => ({
  default: (props: Record<string, unknown>) => {
    playerState.props = props;
    const chapters = props.chapters as readonly { title: string }[] | undefined;
    const bookmarks = props.bookmarks as readonly { title: string; revision: number }[] | undefined;
    return (
      <div
        data-testid="video-player"
        data-chapters={chapters?.map((item) => item.title).join('|') ?? ''}
        data-bookmarks={bookmarks?.map((item) => `${item.title}:${item.revision}`).join('|') ?? ''}
        data-marker-context={String(props.markerContextKey ?? '')}
      />
    );
  },
}));

function media(id: number) {
  return {
    id,
    library_id: 1,
    file_path: `D:/video/${id}.mp4`,
    file_name: `${id}.mp4`,
    file_size: 100,
    format: 'mp4',
    video_codec: 'h264',
    audio_codec: 'aac',
    duration: 100,
    width: 640,
    height: 360,
    bitrate: 1000,
    subtitle_tracks: '',
    added_at: '',
    modified_at: '',
  };
}

function useBaseHandlers() {
  server.use(
    http.get('*/api/library/media/:id', ({ params }) =>
      HttpResponse.json(media(Number(params.id))),
    ),
    http.get('*/api/play/:id/tracks', () =>
      HttpResponse.json({
        backend: {},
        selection: {
          audio: { effective_track_id: null, selected_track_id: null },
          subtitle: { effective_track_id: null, selected_track_id: null },
        },
        sources: {},
        tracks: [],
      }),
    ),
    http.get('*/api/play/:id/timeline-preview', () =>
      HttpResponse.json({ duration: 0, profile_id: 'timeline-v1', status: 'pending', version: 1 }, { status: 202 }),
    ),
  );
}

function renderPage(route = '/play/1') {
  const router = createMemoryRouter(
    [
      { path: '/play/:id', element: <PlayPage /> },
      { path: '/other', element: <div>其它页面</div> },
    ],
    { initialEntries: [route] },
  );
  return {
    router,
    ...render(
      <MantineProvider>
        <RouterProvider router={router} />
      </MantineProvider>,
    ),
  };
}

function chapter(title: string, startMs = 0) {
  return {
    endMs: startMs + 10_000,
    id: `chapter-${title}`,
    language: 'zh',
    source: 'embedded' as const,
    sourceIndex: startMs / 10_000,
    startMs,
    title,
  };
}

function bookmark(title: string, revision: number) {
  return {
    createdAt: '2026-07-13T00:00:00Z',
    id: 'bookmark-1',
    note: '服务端备注',
    positionMs: 5_000,
    revision,
    title,
    updatedAt: '2026-07-13T00:01:00Z',
  };
}

beforeEach(() => {
  vi.restoreAllMocks();
  playerState.props = {};
  for (const mock of Object.values(mediaClient)) mock.mockReset();
  mediaClient.getMediaChapters.mockResolvedValue({ items: [], parsedAt: null, stale: false });
  mediaClient.listMediaBookmarks.mockResolvedValue([]);
  useBaseHandlers();
});

describe('PlayPage 章节与书签数据接线（FR2-060）', () => {
  it('复用 media-client 契约加载章节和书签并注入共享播放器', async () => {
    mediaClient.getMediaChapters.mockResolvedValue({
      items: [chapter('开场')],
      parsedAt: '2026-07-13T00:00:00Z',
      stale: false,
    });
    mediaClient.listMediaBookmarks.mockResolvedValue([bookmark('重点', 1)]);

    renderPage();

    const player = await screen.findByTestId('video-player');
    await waitFor(() => {
      expect(player).toHaveAttribute('data-chapters', '开场');
      expect(player).toHaveAttribute('data-bookmarks', '重点:1');
      expect(player).toHaveAttribute('data-marker-context', 'space-default:1');
    });
    expect(mediaClient.getMediaChapters).toHaveBeenCalledWith(
      expect.objectContaining({ space: { spaceId: 'space-default' } }),
      1,
    );
    expect(mediaClient.listMediaBookmarks).toHaveBeenCalledWith(expect.any(Object), 1);
  });

  it('revision CAS 冲突后重新加载服务端真源并显示明确提示', async () => {
    mediaClient.listMediaBookmarks
      .mockResolvedValueOnce([bookmark('旧端标题', 1)])
      .mockResolvedValue([bookmark('服务端标题', 2)]);
    mediaClient.updateMediaBookmark.mockImplementation(
      async (
        _client: unknown,
        _mediaId: number,
        _bookmarkId: string,
        _input: MediaBookmarkUpdate,
        options: BookmarkMutationOptions,
      ) => {
        await options.reload();
        throw new BookmarkConflictError(
          '书签已被其他客户端修改或删除',
          bookmark('服务端标题', 2),
          false,
        );
      },
    );
    const notify = vi.spyOn(notifications, 'show');
    renderPage();
    const player = await screen.findByTestId('video-player');
    await waitFor(() => expect(player).toHaveAttribute('data-bookmarks', '旧端标题:1'));

    const update = playerState.props.onUpdateBookmark as (
      bookmarkId: string,
      input: MediaBookmarkUpdate,
    ) => Promise<void>;
    await act(async () => {
      await update('bookmark-1', {
        note: null,
        positionMs: 5_000,
        revision: 1,
        title: '尝试覆盖',
      });
    });

    await waitFor(() => expect(player).toHaveAttribute('data-bookmarks', '服务端标题:2'));
    expect(mediaClient.listMediaBookmarks).toHaveBeenCalledTimes(2);
    expect(notify).toHaveBeenCalledWith(
      expect.objectContaining({
        color: 'yellow',
        message: '已重新加载服务端最新书签，未覆盖其他设备的修改',
        title: '书签已在其他设备更新',
      }),
    );
  });

  it('快速切换媒体时丢弃旧章节和书签的迟到响应', async () => {
    let releaseOld!: () => void;
    const oldGate = new Promise<void>((resolve) => {
      releaseOld = resolve;
    });
    mediaClient.getMediaChapters.mockImplementation(async (_client: unknown, mediaId: number) => {
      if (mediaId === 1) await oldGate;
      return {
        items: [chapter(mediaId === 1 ? '旧章节' : '新章节')],
        parsedAt: null,
        stale: false,
      };
    });
    mediaClient.listMediaBookmarks.mockImplementation(async (_client: unknown, mediaId: number) => {
      if (mediaId === 1) await oldGate;
      return [bookmark(mediaId === 1 ? '旧书签' : '新书签', 1)];
    });

    const { router } = renderPage('/play/1');
    await screen.findByTestId('video-player');
    await act(async () => {
      await router.navigate('/play/2');
    });
    const player = await screen.findByTestId('video-player');
    await waitFor(() => {
      expect(player).toHaveAttribute('data-chapters', '新章节');
      expect(player).toHaveAttribute('data-bookmarks', '新书签:1');
      expect(player).toHaveAttribute('data-marker-context', 'space-default:2');
    });

    releaseOld();
    await act(async () => Promise.resolve());
    expect(player).toHaveAttribute('data-chapters', '新章节');
    expect(player).toHaveAttribute('data-bookmarks', '新书签:1');
  });
});
