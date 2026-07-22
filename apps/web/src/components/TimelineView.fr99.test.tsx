import { render, screen, fireEvent, within } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import TimelineView from './TimelineView';
import type { MediaFile } from '@/types';

// jsdom 无 IntersectionObserver，提供空实现避免组件挂载哨兵时崩溃
class MockIO {
  observe() {}
  unobserve() {}
  disconnect() {}
}

// 视频文件：同一天，便于落在同一可见分组
const video = (id: number, over: Partial<MediaFile> = {}): MediaFile => ({
  id,
  library_id: 1,
  file_path: `/x/v${id}.mp4`,
  file_name: `v${id}.mp4`,
  file_size: 1024 * 1024,
  format: 'mp4',
  video_codec: 'h264',
  audio_codec: 'aac',
  duration: 95,
  width: 1920,
  height: 1080,
  bitrate: 0,
  subtitle_tracks: '',
  added_at: '2025-01-09T00:00:00Z',
  modified_at: '2025-01-09T00:00:00Z',
  ...over,
});
const image = (id: number): MediaFile =>
  video(id, { format: 'png', file_name: `p${id}.png`, duration: 0 });

function renderView(props: Partial<React.ComponentProps<typeof TimelineView>> = {}) {
  return render(
    <MantineProvider>
      <TimelineView
        mediaFiles={[video(1), image(2)]}
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

describe('TimelineView 媒体卡与网格重设计（FR-99）', () => {
  beforeEach(() => {
    (globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO;
  });

  it('视频卡渲染时长角标与中心播放叠层；图片卡不渲染', () => {
    renderView();
    // 视频：时长角标 1:35 + 中心播放叠层（aria-label="视频"）
    expect(screen.getByText('1:35')).toBeTruthy();
    const playOverlays = screen.getAllByLabelText('视频');
    expect(playOverlays.length).toBe(1);
  });

  it('卡片 hover 浮层含播放/收藏/更多操作', () => {
    renderView({ onToggleFavorite: vi.fn(), onBatchDelete: vi.fn() });
    // 浮层操作以无障碍标签暴露，始终在 DOM（hover 仅控制 CSS 可见性）
    expect(screen.getAllByLabelText('播放').length).toBeGreaterThan(0);
    expect(screen.getAllByLabelText('收藏').length).toBeGreaterThan(0);
    expect(screen.getAllByLabelText('更多操作').length).toBeGreaterThan(0);
  });

  it('选中项渲染品牌紫勾选叠层（强反馈）', () => {
    renderView({ onSelectionChange: vi.fn() });
    fireEvent.click(screen.getByText('v1.mp4'), { ctrlKey: true });
    // 选中态叠层
    expect(screen.getByLabelText('已选中')).toBeTruthy();
  });

  it('选中 ≥1 项时浮现 sticky 批量条，含批量按钮且回调可触发', () => {
    const onBatchDelete = vi.fn();
    const onBatchAddToAlbum = vi.fn();
    renderView({
      onBatchDelete,
      onBatchAddToAlbum,
      onBatchAddTag: vi.fn(),
      onBatchDownload: vi.fn(),
    });
    // 未选中：无批量条
    expect(screen.queryByRole('toolbar', { name: '批量操作' })).toBeNull();
    // Ctrl 选中一项后批量条出现
    fireEvent.click(screen.getByText('v1.mp4'), { ctrlKey: true });
    const bar = screen.getByRole('toolbar', { name: '批量操作' });
    expect(within(bar).getByText(/已选\s*1\s*项/)).toBeTruthy();
    // 批量按钮存在且可触发
    fireEvent.click(within(bar).getByRole('button', { name: '加入相册' }));
    expect(onBatchAddToAlbum).toHaveBeenCalledWith([1]);
    fireEvent.click(within(bar).getByRole('button', { name: '删除' }));
    expect(onBatchDelete).toHaveBeenCalledWith([1]);
  });
});
