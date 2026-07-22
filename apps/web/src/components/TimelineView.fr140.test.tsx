import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, vi } from 'vitest';
import TimelineView from './TimelineView';
import type { MediaFile } from '@/types';

// 缩略图左下角常驻收藏（FR-140）：缩略图左下角常驻可点收藏按钮，
// 与左上选择框 / 右下时长角标互不冲突；点击切换收藏且不打开卡片。

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

describe('TimelineView 缩略图左下角常驻收藏（FR-140）', () => {
  it('渲染左下角常驻收藏按钮且点击触发回调、不打开卡片', async () => {
    const onToggleFavorite = vi.fn();
    const onOpenFile = vi.fn();
    renderView({ onToggleFavorite, onOpenFile });

    const btn = screen.getByRole('button', { name: '收藏' });
    // 左下角定位：bottom + left
    expect(btn).toHaveStyle({ position: 'absolute', left: '6px', bottom: '6px' });

    await userEvent.click(btn);
    expect(onToggleFavorite).toHaveBeenCalledWith(expect.objectContaining({ id: 1 }));
    expect(onOpenFile).not.toHaveBeenCalled();
  });

  it('已收藏显示为取消收藏', () => {
    renderView({ mediaFiles: [file(1, true)], onToggleFavorite: () => {} });
    expect(screen.getByRole('button', { name: '取消收藏' })).toBeInTheDocument();
  });
});
