import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import ContinueWatching from './ContinueWatching';
import type { MediaFile } from '@/types';

const mockNavigate = vi.fn();

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return { ...actual, useNavigate: () => mockNavigate };
});

// 缩略图组件依赖图片加载，测试中替换为占位，专注续播区块逻辑。
// 不渲染文件名，避免与卡片标题文本重复导致按文本查询命中多个元素。
vi.mock('@/components/MediaThumbnail', () => ({
  default: () => <div data-testid="thumb" />,
}));

function buildMedia(id: number, name: string, lastPosition: number, duration: number): MediaFile {
  return {
    id,
    library_id: 1,
    file_path: `D:/Videos/${name}`,
    file_name: name,
    file_size: 1024,
    format: 'mp4',
    video_codec: 'h264',
    audio_codec: 'aac',
    duration,
    width: 1920,
    height: 1080,
    bitrate: 5000000,
    subtitle_tracks: '',
    added_at: '2025-01-01T12:00:00Z',
    modified_at: '2025-01-01T12:00:00Z',
    last_position: lastPosition,
    watched: false,
    last_watched_at: '2025-01-01T12:00:00Z',
  };
}

function buildWatchItem(media: MediaFile) {
  return {
    media,
    watch_state: {
      completed: false,
      completed_at: null,
      created_at: media.last_watched_at,
      last_event_seq: 1,
      last_session_id: 'test-session',
      last_watched_at: media.last_watched_at,
      media_id: media.id,
      position_seconds: media.last_position,
      revision: 1,
      space_id: 'space-default',
      updated_at: media.last_watched_at,
    },
  };
}

function renderComp() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <ContinueWatching />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('ContinueWatching', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('展示继续观看列表并按点击进入续播', async () => {
    server.use(
      http.get('*/api/library/continue-watching', () =>
        HttpResponse.json({
          items: [buildWatchItem(buildMedia(42, '星际穿越.mp4', 600, 10000))],
        }),
      ),
    );

    renderComp();

    await waitFor(() => {
      expect(screen.getByText('继续观看')).toBeInTheDocument();
      expect(screen.getByText('星际穿越.mp4')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /继续观看 星际穿越\.mp4/ }));
    expect(mockNavigate).toHaveBeenCalledWith('/play/42');
  });

  it('列表为空时不渲染区块', async () => {
    server.use(http.get('*/api/library/continue-watching', () => HttpResponse.json({ items: [] })));

    renderComp();

    // 等待请求完成后仍不出现标题
    await waitFor(() => {
      expect(screen.queryByTestId('continue-watching')).not.toBeInTheDocument();
    });
    expect(screen.queryByText('继续观看')).not.toBeInTheDocument();
  });
});
