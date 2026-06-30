import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import TimelineScrubber from './TimelineScrubber';
import type { DateGroup } from '@/utils/timeline';

// FR-143 移动端交互：窄屏（compact）下时间轴标尺改为防误触形态——
// 隐藏 12px 可拖动细轨（屏幕边缘易误触），改渲染可点击年份标签快速导航。

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
  makeGroup('2025-03-15', [1]), // 下标 0：2025 年最新分组
  makeGroup('2025-02-10', [2]),
  makeGroup('2024-12-20', [3]), // 下标 2：2024 年最新分组
  makeGroup('2024-11-01', [4]),
];

function renderCompact(onSeek = vi.fn()) {
  const utils = render(
    <MantineProvider>
      <TimelineScrubber groups={groups} onSeek={onSeek} compact />
    </MantineProvider>,
  );
  return { ...utils, onSeek };
}

describe('TimelineScrubber 窄屏防误触（FR-143）', () => {
  beforeEach(() => vi.clearAllMocks());

  it('compact 模式不渲染可拖动细轨（slider 角色缺席）', () => {
    renderCompact();
    expect(screen.queryByRole('slider')).not.toBeInTheDocument();
  });

  it('compact 模式渲染可点击年份快速导航按钮', () => {
    renderCompact();
    expect(screen.getByRole('button', { name: '跳到 2025 年' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '跳到 2024 年' })).toBeInTheDocument();
  });

  it('点击年份按钮跳到该年最新分组', () => {
    const { onSeek } = renderCompact();
    fireEvent.click(screen.getByRole('button', { name: '跳到 2024 年' }));
    // 2024 年最新分组为 2024-12-20，下标 2
    expect(onSeek).toHaveBeenCalledWith(2);
  });

  it('点击 2025 年按钮跳到下标 0', () => {
    const { onSeek } = renderCompact();
    fireEvent.click(screen.getByRole('button', { name: '跳到 2025 年' }));
    expect(onSeek).toHaveBeenCalledWith(0);
  });

  it('空分组 compact 下也不渲染', () => {
    render(
      <MantineProvider>
        <TimelineScrubber groups={[]} onSeek={vi.fn()} compact />
      </MantineProvider>,
    );
    expect(screen.queryByRole('button', { name: /跳到/ })).not.toBeInTheDocument();
  });

  it('非 compact（桌面）仍渲染可拖动细轨', () => {
    render(
      <MantineProvider>
        <TimelineScrubber groups={groups} onSeek={vi.fn()} />
      </MantineProvider>,
    );
    expect(screen.getByRole('slider')).toBeInTheDocument();
  });
});
