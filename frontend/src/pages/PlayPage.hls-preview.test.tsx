import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
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

describe('PlayPage HLS preview fallback', () => {
  it('先直连播放，直连失败后才切换已生成 HLS', async () => {
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
      http.get('*/api/play/1/hls-status', () =>
        HttpResponse.json({
          available: true,
          profile_id: 'h264',
          url: '/api/play/hls/1/master.m3u8',
          task: null,
        }),
      ),
    );

    renderPage();
    const player = await screen.findByTestId('video-player');
    expect(player).toHaveAttribute('data-url', expect.stringContaining('/api/play/1/stream'));
    expect(playbackError).toBeTypeOf('function');
    playbackError?.();
    await waitFor(() =>
      expect(screen.getByTestId('video-player')).toHaveAttribute(
        'data-url',
        expect.stringContaining('/api/play/hls/1/master.m3u8'),
      ),
    );
  });
});
