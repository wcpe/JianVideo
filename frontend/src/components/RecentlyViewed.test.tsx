import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import RecentlyViewed from './RecentlyViewed';
import type { MediaFile } from '@/types';

const mockNavigate = vi.fn();

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return { ...actual, useNavigate: () => mockNavigate };
});

// 缩略图组件依赖图片加载，测试中替换为占位，专注最近查看区块逻辑。
vi.mock('@/components/MediaThumbnail', () => ({
  default: () => <div data-testid="thumb" />,
}));

// 构造一条最近查看媒体，含 last_viewed_at；display_name 优先于 file_name 展示（FR-30）。
function buildViewed(id: number, name: string, displayName?: string): MediaFile {
  return {
    id,
    library_id: 1,
    file_path: `D:/Photos/${name}`,
    file_name: name,
    file_size: 1024,
    format: 'jpg',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 1920,
    height: 1080,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: '2025-01-01T12:00:00Z',
    modified_at: '2025-01-01T12:00:00Z',
    last_viewed_at: '2025-06-01T12:00:00Z',
    display_name: displayName,
  };
}

function renderComp() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <RecentlyViewed />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('RecentlyViewed', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('展示最近查看媒体并以展示名标注，点击进入查看', async () => {
    server.use(
      http.get('*/api/library/recently-viewed', () =>
        HttpResponse.json({ items: [buildViewed(9, '海边日落.jpg', '我的日落')] }),
      ),
    );

    renderComp();

    await waitFor(() => {
      expect(screen.getByText('最近查看')).toBeInTheDocument();
      // 展示名优先（FR-30）
      expect(screen.getByText('我的日落')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /最近查看 我的日落/ }));
    expect(mockNavigate).toHaveBeenCalledWith('/play/9');
  });

  it('列表为空时不渲染区块', async () => {
    server.use(http.get('*/api/library/recently-viewed', () => HttpResponse.json({ items: [] })));

    renderComp();

    await waitFor(() => {
      expect(screen.queryByTestId('recently-viewed')).not.toBeInTheDocument();
    });
    expect(screen.queryByText('最近查看')).not.toBeInTheDocument();
  });

  it('请求失败时静默不渲染区块', async () => {
    server.use(
      http.get('*/api/library/recently-viewed', () =>
        HttpResponse.json({ code: 'INTERNAL' }, { status: 500 }),
      ),
    );

    renderComp();

    // 拉取失败兜底为空列表、整块不渲染
    await waitFor(() => {
      expect(screen.queryByText('最近查看')).not.toBeInTheDocument();
    });
  });
});
