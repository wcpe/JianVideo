import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import PixiMediaGrid from './PixiMediaGrid';
import type { MediaFile } from '@/types';

const session = {
  setItems: vi.fn(),
  setViewport: vi.fn(),
  setSelection: vi.fn(),
  setLayout: vi.fn(),
  resize: vi.fn(),
  getMetrics: vi.fn(() => ({
    visibleItems: 2,
    pixiObjectCount: 2,
    textureCount: 0,
    textureMemoryBytes: 0,
    thumbnailRequests: 0,
    hlsRequests: 0,
  })),
  getFrame: vi.fn(),
  destroy: vi.fn(),
  canvas: document.createElement('canvas'),
  pixiVersion: 'test',
  rendererType: 'webgl-test',
};

vi.mock('@jianvideo/render-pixi', () => ({
  mountMediaGridSession: vi.fn(async ({ host }: { host: HTMLElement }) => {
    host.replaceChildren(session.canvas);
    return session;
  }),
}));

function media(id: number): MediaFile {
  return {
    id,
    library_id: 1,
    file_path: `D:/v/${id}.mp4`,
    file_name: `${id}.mp4`,
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
}

describe('PixiMediaGrid (FR2-009)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('挂载会话并同步 items/选中态', async () => {
    render(
      <MantineProvider>
        <PixiMediaGrid
          items={[media(1), media(2)]}
          total={2}
          selectedIds={new Set([1])}
          onSelect={vi.fn()}
          onOpen={vi.fn()}
        />
      </MantineProvider>,
    );

    expect(await screen.findByTestId('pixi-media-grid')).toBeInTheDocument();
    await waitFor(() => {
      expect(session.setItems).toHaveBeenCalled();
      expect(session.setSelection).toHaveBeenCalledWith({ selectedIds: new Set([1]) });
    });
    expect(screen.getByText(/共 2 项/)).toBeVisible();
  });

  it('卸载时销毁会话', async () => {
    const { unmount } = render(
      <MantineProvider>
        <PixiMediaGrid
          items={[media(1)]}
          total={1}
          selectedIds={new Set()}
          onSelect={vi.fn()}
          onOpen={vi.fn()}
        />
      </MantineProvider>,
    );
    await screen.findByTestId('pixi-media-grid');
    unmount();
    expect(session.destroy).toHaveBeenCalled();
  });
});
