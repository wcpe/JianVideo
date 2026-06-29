import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, vi } from 'vitest';
import TimelineView from './TimelineView';
import type { MediaFile } from '@/types';

const file = (id: number, favorite = false): MediaFile => ({
  id,
  library_id: 1,
  file_path: `/x/f${id}.png`,
  file_name: `f${id}.png`,
  file_size: 1,
  format: 'png',
  video_codec: '',
  audio_codec: '',
  duration: 0,
  width: 1,
  height: 1,
  bitrate: 0,
  subtitle_tracks: '',
  added_at: '2025-01-09T00:00:00Z',
  modified_at: '2025-01-09T00:00:00Z',
  favorite,
});

function renderView(props: Partial<React.ComponentProps<typeof TimelineView>> = {}) {
  return render(
    <MantineProvider>
      <TimelineView
        mediaFiles={[file(1)]}
        loading={false}
        error={null}
        customImageExtensions={{}}
        onErrorClose={() => {}}
        onOpenFile={() => {}}
        {...props}
      />
    </MantineProvider>,
  );
}

describe('TimelineView 收藏标星', () => {
  it('点击标星按钮触发 onToggleFavorite 且不触发 onOpenFile', async () => {
    const onToggleFavorite = vi.fn();
    const onOpenFile = vi.fn();
    renderView({ onToggleFavorite, onOpenFile });

    const btn = screen.getByRole('button', { name: '收藏' });
    await userEvent.click(btn);

    expect(onToggleFavorite).toHaveBeenCalledWith(expect.objectContaining({ id: 1 }));
    // 标星按钮点击应阻止冒泡，不触发卡片打开
    expect(onOpenFile).not.toHaveBeenCalled();
  });

  it('已收藏的媒体标星按钮显示为取消收藏', () => {
    renderView({ mediaFiles: [file(1, true)], onToggleFavorite: () => {} });
    expect(screen.getByRole('button', { name: '取消收藏' })).toBeInTheDocument();
  });
});
