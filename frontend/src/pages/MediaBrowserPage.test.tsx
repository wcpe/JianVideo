import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import MediaBrowserPage from './MediaBrowserPage';

vi.mock('@/components/PixiMediaGrid', () => ({
  default: (props: { total: number; items: { id: number }[] }) => (
    <div
      data-testid="pixi-media-grid-stub"
      data-total={props.total}
      data-count={props.items.length}
    />
  ),
}));

vi.mock('@/hooks/useLibraryPaths', () => ({
  useLibraryPaths: () => ({ paths: [], customImageExtensions: {} }),
}));

vi.mock('@/hooks/useBatchActions', () => ({
  useBatchActions: () => ({
    openAddToAlbum: vi.fn(),
    openTranscode: vi.fn(),
    openMove: vi.fn(),
    openAddTag: vi.fn(),
    download: vi.fn(),
    modalState: {
      albumOpened: false,
      albums: [],
      loadingAlbums: false,
      confirmAlbum: vi.fn(),
      closeAlbum: vi.fn(),
      tagOpened: false,
      tags: [],
      loadingTags: false,
      confirmTag: vi.fn(),
      closeTag: vi.fn(),
      transcodeOpened: false,
      presets: [],
      loadingPresets: false,
      confirmTranscode: vi.fn(),
      closeTranscode: vi.fn(),
      moveOpened: false,
      libraries: [],
      loadingLibraries: false,
      confirmMove: vi.fn(),
      closeMove: vi.fn(),
    },
  }),
}));

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/media-grid']}>
        <MediaBrowserPage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('MediaBrowserPage (FR2-009)', () => {
  beforeEach(() => {
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({
          items: [
            {
              id: 1,
              library_id: 1,
              file_path: 'D:/a.mp4',
              file_name: 'a.mp4',
              file_size: 100,
              format: 'mp4',
              video_codec: 'h264',
              audio_codec: 'aac',
              duration: 10,
              width: 640,
              height: 360,
              bitrate: 1000,
              subtitle_tracks: '',
              added_at: '2025-01-01T00:00:00Z',
              modified_at: '2025-01-01T00:00:00Z',
            },
          ],
          total: 1,
          page: 1,
          page_size: 120,
        }),
      ),
      http.get('*/api/library/tags', () => HttpResponse.json({ items: [] })),
      http.get('*/api/library/paths', () => HttpResponse.json({ items: [] })),
    );
  });

  it('渲染媒体网格页与 Pixi 热区', async () => {
    renderPage();
    expect(await screen.findByRole('heading', { name: '媒体网格' })).toBeVisible();
    await waitFor(() => {
      const grid = screen.getByTestId('pixi-media-grid-stub');
      expect(grid).toHaveAttribute('data-total', '1');
      expect(grid).toHaveAttribute('data-count', '1');
    });
  });

  it('提供列数与排序控件', async () => {
    renderPage();
    expect(await screen.findByLabelText('网格列数')).toBeVisible();
    expect(screen.getByLabelText('排序')).toBeVisible();
  });
});
