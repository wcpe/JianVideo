import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { MantineProvider } from '@mantine/core';
import TimelineScrubber from './TimelineScrubber';
import type { DateGroup } from '@/utils/timeline';

// FR-138（ADR-0049）：scrubber 升级为唯一完整时间轴导航条——除 hover/拖动预览外，
// 轨道旁常驻年/月刻度标记，帮助用户在不交互时也能快速定位时间段。

function makeGroup(date: string, ids: number[]): DateGroup {
  return {
    date,
    files: ids.map((id) => ({
      id,
      library_id: 1,
      file_path: `D:\\Photos\\${id}.jpg`,
      file_name: `${id}.jpg`,
      file_size: 0,
      format: 'jpg',
      video_codec: '',
      audio_codec: '',
      duration: 0,
      width: 0,
      height: 0,
      bitrate: 0,
      subtitle_tracks: '',
      added_at: `${date}T00:00:00Z`,
      modified_at: `${date}T00:00:00Z`,
    })),
  };
}

const groups: DateGroup[] = [
  makeGroup('2025-03-15', [1]),
  makeGroup('2025-02-10', [2]),
  makeGroup('2024-12-20', [3]),
  makeGroup('2024-11-01', [4]),
];

function renderScrubber() {
  return render(
    <MantineProvider>
      <TimelineScrubber groups={groups} onSeek={() => {}} />
    </MantineProvider>,
  );
}

describe('TimelineScrubber 年/月刻度（FR-138）', () => {
  it('常驻渲染年份刻度标记（每个出现的年份至少一个）', () => {
    renderScrubber();
    const ticks = screen.getByTestId('timeline-ticks');
    // 跨 2025 与 2024 两个年份，各应有年份刻度文本
    expect(ticks).toHaveTextContent('2025');
    expect(ticks).toHaveTextContent('2024');
  });

  it('刻度区不拦截轨道指针交互（pointer-events:none）', () => {
    renderScrubber();
    const ticks = screen.getByTestId('timeline-ticks');
    expect(ticks.style.pointerEvents).toBe('none');
  });
});
