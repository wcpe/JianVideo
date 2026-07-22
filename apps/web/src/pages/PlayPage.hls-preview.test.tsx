import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import PlayPage from './PlayPage';

let playbackError: (() => void) | undefined;

vi.mock('@/components/VideoPlayer', () => ({
  default: (props: { url: string; onPlaybackError?: () => void }) => {
    playbackError = props.onPlaybackError;
    return <div data-testid="video-player" data-url={props.url} />;
  },
}));

function renderPage() {
  const router = createMemoryRouter([{ path: '/play/:id', element: <PlayPage /> }], {
    initialEntries: ['/play/1'],
  });
  return render(
    <MantineProvider>
      <RouterProvider router={router} />
    </MantineProvider>,
  );
}

describe('PlayPage ABR fallback', () => {
  beforeEach(() => {
    server.use(
      http.get('*/api/play/:id/timeline-preview', () =>
        HttpResponse.json(
          { duration: 0, profile_id: 'timeline-v1', status: 'pending', version: 1 },
          { status: 202 },
        ),
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
    );
  });

  it('先直连播放，失败后只查询已生成 ABR，不自动入队', async () => {
    let requestedProfile = '';
    let enqueueCount = 0;
    server.use(
      http.get('*/api/library/media/1', () =>
        HttpResponse.json({
          id: 1,
          library_id: 1,
          file_path: 'D:/video/source.mp4',
          file_name: 'source.mp4',
          file_size: 100,
          format: 'mp4',
          video_codec: 'h264',
          audio_codec: 'aac',
          duration: 1,
          width: 640,
          height: 360,
          bitrate: 1000,
          subtitle_tracks: '',
          added_at: '',
          modified_at: '',
        }),
      ),
      http.get('*/api/play/1/hls-status', ({ request }) => {
        requestedProfile = new URL(request.url).searchParams.get('profile_id') || '';
        return HttpResponse.json({
          available: true,
          profile_id: 'abr-h264',
          url: '/api/play/hls/1/profiles/abr-h264/master.m3u8',
          task: null,
        });
      }),
      http.post('*/api/play/1/hls-abr', () => {
        enqueueCount += 1;
        return HttpResponse.json({}, { status: 202 });
      }),
    );

    renderPage();
    const player = await screen.findByTestId('video-player');
    expect(player).toHaveAttribute('data-url', expect.stringContaining('/api/play/1/stream'));
    expect(playbackError).toBeTypeOf('function');
    playbackError?.();
    await waitFor(() =>
      expect(screen.getByTestId('video-player')).toHaveAttribute(
        'data-url',
        expect.stringContaining('/api/play/hls/1/profiles/abr-h264/master.m3u8'),
      ),
    );
    expect(requestedProfile).toBe('abr-h264');
    expect(enqueueCount).toBe(0);
  });

  it('ABR 不可用时继续回退到已生成的单档 HLS', async () => {
    const requestedProfiles: string[] = [];
    server.use(
      http.get('*/api/library/media/1', () =>
        HttpResponse.json({
          id: 1,
          library_id: 1,
          file_path: 'D:/video/source.mp4',
          file_name: 'source.mp4',
          file_size: 100,
          format: 'mp4',
          video_codec: 'h264',
          audio_codec: 'aac',
          duration: 1,
          width: 640,
          height: 360,
          bitrate: 1000,
          subtitle_tracks: '',
          added_at: '',
          modified_at: '',
        }),
      ),
      http.get('*/api/play/1/hls-status', ({ request }) => {
        const profileID = new URL(request.url).searchParams.get('profile_id') || '';
        requestedProfiles.push(profileID);
        return HttpResponse.json({
          available: profileID === 'h264',
          profile_id: profileID,
          url:
            profileID === 'h264'
              ? '/api/play/hls/1/master.m3u8'
              : '/api/play/hls/1/profiles/abr-h264/master.m3u8',
          task: null,
        });
      }),
    );

    renderPage();
    await screen.findByTestId('video-player');
    playbackError?.();

    await waitFor(() =>
      expect(screen.getByTestId('video-player')).toHaveAttribute(
        'data-url',
        expect.stringContaining('/api/play/hls/1/master.m3u8'),
      ),
    );
    expect(requestedProfiles).toEqual(['abr-h264', 'h264']);
  });
});
