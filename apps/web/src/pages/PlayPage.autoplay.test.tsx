import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import PlayPage from './PlayPage';

let onEnded: (() => void) | undefined;

vi.mock('@/components/VideoPlayer', () => ({
  default: (props: { onEnded?: () => void }) => {
    onEnded = props.onEnded;
    return <div data-testid="video-player" />;
  },
}));

vi.mock('@/hooks/cinema-context', () => ({
  useCinemaMode: () => ({ cinema: false, setCinema: vi.fn() }),
}));

const mediaFixture = {
  id: 1,
  library_id: 1,
  space_id: 'space-default',
  file_path: 'D:/video/S01E01.mp4',
  file_name: 'S01E01.mp4',
  file_size: 100,
  format: 'mp4',
  video_codec: 'h264',
  audio_codec: 'aac',
  duration: 10,
  width: 640,
  height: 360,
  bitrate: 1000,
  subtitle_tracks: '',
  added_at: '',
  modified_at: '',
};

function baseHandlers() {
  return [
    http.get('*/api/library/media/:id', ({ params }) => {
      const id = Number(params.id);
      return HttpResponse.json({ ...mediaFixture, id, file_name: `S01E0${id}.mp4` });
    }),
    http.get('*/api/library/media/:id/inference', () =>
      HttpResponse.json({
        inference: {
          id: 1,
          media_id: 1,
          space_id: 'space-default',
          kind: 'series',
          title: '测试剧',
          year: 0,
          season: 1,
          episode: 1,
          episode_title: '',
          confidence: 1,
          source: 'manual',
          rule_version: 'fr2-031-v1',
          manual: true,
          created_at: '',
          updated_at: '',
        },
      }),
    ),
    http.get('*/api/play/:id/tracks', () =>
      HttpResponse.json({
        tracks: [],
        selection: {
          audio: { selected_track_id: null, effective_track_id: null },
          subtitle: { selected_track_id: null, effective_track_id: null },
        },
        sources: {},
        backend: {},
      }),
    ),
    http.get('*/api/play/:id/timeline-preview', () =>
      HttpResponse.json(
        { duration: 0, profile_id: 'timeline-v1', status: 'pending', version: 1 },
        { status: 202 },
      ),
    ),
    http.get('*/api/play/:id/watch-state', () => HttpResponse.json(null, { status: 404 })),
    http.put('*/api/library/media/:id/viewed', () => new HttpResponse(null, { status: 204 })),
    http.get('*/api/play/:id/negotiate', () =>
      HttpResponse.json({ path: 'mp4', url: '/api/play/1/stream' }),
    ),
  ];
}

function renderPlay(entry = '/play/1') {
  const router = createMemoryRouter([{ path: '/play/:id', element: <PlayPage /> }], {
    initialEntries: [entry],
  });
  return {
    router,
    ...render(
      <MantineProvider>
        <RouterProvider router={router} />
      </MantineProvider>,
    ),
  };
}

describe('PlayPage 连播 (FR2-047)', () => {
  beforeEach(() => {
    localStorage.clear();
    onEnded = undefined;
    server.use(...baseHandlers());
  });

  it('开启连播时 ended 跳转下一集', async () => {
    server.use(
      http.get('*/api/library/media/1/next-episode', () =>
        HttpResponse.json({
          media: { ...mediaFixture, id: 2, file_name: 'S01E02.mp4' },
          current: { title: '测试剧', season: 1, episode: 1 },
          next: { title: '测试剧', season: 1, episode: 2, media_id: 2 },
        }),
      ),
    );
    const { router } = renderPlay('/play/1');
    await screen.findByTestId('video-player');
    expect(onEnded).toBeTypeOf('function');
    onEnded?.();
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/play/2');
    });
  });

  it('关闭自动连播后 ended 不跳转', async () => {
    localStorage.setItem('jianvideo-autoplay-next', '0');
    let nextCalled = false;
    server.use(
      http.get('*/api/library/media/1/next-episode', () => {
        nextCalled = true;
        return HttpResponse.json({ media: { ...mediaFixture, id: 2 }, current: null, next: null });
      }),
    );
    const { router } = renderPlay('/play/1');
    await screen.findByTestId('video-player');
    const toggle = await screen.findByRole('switch', { name: '自动连播' });
    expect(toggle).not.toBeChecked();
    onEnded?.();
    await new Promise((r) => setTimeout(r, 80));
    expect(nextCalled).toBe(false);
    expect(router.state.location.pathname).toBe('/play/1');
  });

  it('合集上下文 ended 走邻项 API', async () => {
    server.use(
      http.get('*/api/albums/9/neighbor', ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get('media_id')).toBe('1');
        expect(url.searchParams.get('dir')).toBe('next');
        return HttpResponse.json({ media: { ...mediaFixture, id: 3, file_name: 'b.mp4' } });
      }),
    );
    const { router } = renderPlay('/play/1?albumId=9');
    await screen.findByTestId('video-player');
    expect(await screen.findByLabelText('下一首')).toBeVisible();
    onEnded?.();
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/play/3');
      expect(router.state.location.search).toBe('?albumId=9');
    });
  });

  it('切换自动连播开关写入 localStorage', async () => {
    const user = userEvent.setup();
    renderPlay('/play/1');
    const toggle = await screen.findByRole('switch', { name: '自动连播' });
    expect(toggle).toBeChecked();
    await user.click(toggle);
    expect(localStorage.getItem('jianvideo-autoplay-next')).toBe('0');
  });
});
