import { render, screen } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, beforeEach } from 'vitest';
import TimelineView from './TimelineView';
import type { MediaFile } from '@/types';

// FR-138 时间轴导航形态重构（ADR-0049）：
// - 移除左侧竖向日期轴（不再有竖轴锚点圆点 + 竖线列）。
// - 日期分组改为顶部横向分组头（网格占满主区）。
// - 右侧 TimelineScrubber 为唯一完整时间轴导航条。

class MockIO {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const file = (id: number, mediaTime: string): MediaFile => ({
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
  media_time: mediaTime,
});

function renderView(props: Partial<React.ComponentProps<typeof TimelineView>> = {}) {
  return render(
    <MantineProvider>
      <TimelineView
        mediaFiles={[file(1, '2025-03-15T00:00:00Z'), file(2, '2025-02-10T00:00:00Z')]}
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

describe('TimelineView 导航形态重构（FR-138）', () => {
  beforeEach(() => {
    (globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO;
  });

  it('不再渲染左侧竖向日期轴锚点（data-timeline-axis 已移除）', () => {
    const { container } = renderView();
    expect(container.querySelector('[data-timeline-axis]')).toBeNull();
  });

  it('日期分组头以横向形式展示分组日期', () => {
    renderView();
    // 分组头展示分组键（日粒度 YYYY-MM-DD），且标注 data-timeline-group-header
    const header = screen.getByText('2025-03-15').closest('[data-timeline-group-header]');
    expect(header).not.toBeNull();
  });

  it('仍渲染右侧时间轴 scrubber 作为唯一导航条', () => {
    renderView();
    expect(screen.getByRole('slider', { name: '时间轴拖动定位' })).toBeInTheDocument();
  });
});
