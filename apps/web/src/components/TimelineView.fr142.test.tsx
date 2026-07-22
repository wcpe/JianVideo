import { render, screen } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { createRef } from 'react';
import TimelineView, { type TimelineViewHandle } from './TimelineView';
import type { MediaFile } from '@/types';

// FR-142 时间轴精准定位：
// - TimelineView 暴露 scrollToDate（按日期跳转）与 getTopVisibleGroupDate（视口锁定）命令式句柄。

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

// 三天分组（倒序）：2025-03-15 / 2025-02-10 / 2025-01-05
const files = [
  file(1, '2025-03-15T00:00:00Z'),
  file(2, '2025-02-10T00:00:00Z'),
  file(3, '2025-01-05T00:00:00Z'),
];

function renderWithRef(ref: React.Ref<TimelineViewHandle>) {
  return render(
    <MantineProvider>
      <TimelineView
        ref={ref}
        mediaFiles={files}
        loading={false}
        error={null}
        customImageExtensions={{}}
        onErrorClose={() => {}}
        onOpenFile={() => {}}
      />
    </MantineProvider>,
  );
}

describe('TimelineView 命令式句柄（FR-142）', () => {
  beforeEach(() => {
    (globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO;
  });

  it('scrollToDate 解析日期并返回命中分组下标（非法/无命中返回 false）', () => {
    const ref = createRef<TimelineViewHandle>();
    renderWithRef(ref);
    expect(ref.current).not.toBeNull();
    // 精确命中某日 → 返回 true（已发起滚动）
    expect(ref.current!.scrollToDate('2025-02-10')).toBe(true);
    // 按月命中该月内最新分组 → true
    expect(ref.current!.scrollToDate('2025-01')).toBe(true);
    // 非法查询 → false（不跳转）
    expect(ref.current!.scrollToDate('not-a-date')).toBe(false);
  });

  it('getTopVisibleGroupDate 返回视口顶部分组日期（默认顶部=最新分组）', () => {
    const ref = createRef<TimelineViewHandle>();
    renderWithRef(ref);
    // jsdom 无滚动，默认视口顶部为首个分组（最新）
    expect(ref.current!.getTopVisibleGroupDate()).toBe('2025-03-15');
  });

  it('渲染各日期分组头', () => {
    const ref = createRef<TimelineViewHandle>();
    renderWithRef(ref);
    expect(screen.getByText('2025-03-15')).toBeInTheDocument();
    expect(screen.getByText('2025-01-05')).toBeInTheDocument();
  });
});

// 视口锁定的纯逻辑在 timeline.test.ts 覆盖（resolveDateToGroupIndex / groupDateAtIndex），
// 此处仅验证 TimelineView 句柄把日期正确路由到虚拟化滚动。
describe('TimelineView scrollToDate 路由到虚拟化（FR-142）', () => {
  beforeEach(() => {
    (globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO;
  });

  it('scrollToDate 命中时调用底层滚动', () => {
    const ref = createRef<TimelineViewHandle>();
    renderWithRef(ref);
    // 监听 window.scrollTo（窗口虚拟化最终走它），命中日期应触发一次滚动尝试
    const spy = vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    ref.current!.scrollToDate('2025-01-05');
    expect(spy).toHaveBeenCalled();
    spy.mockRestore();
  });
});
