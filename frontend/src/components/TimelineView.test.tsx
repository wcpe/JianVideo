import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import TimelineView from './TimelineView';
import type { MediaFile } from '@/types';

// 模拟 IntersectionObserver，捕获回调以手动触发“滚到底部哨兵进入视口”
let ioCb: ((entries: Array<{ isIntersecting: boolean }>) => void) | null = null;
class MockIO {
  constructor(cb: (entries: Array<{ isIntersecting: boolean }>) => void) {
    ioCb = cb;
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

const file = (id: number): MediaFile => ({
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
});

function renderView(props: Partial<React.ComponentProps<typeof TimelineView>> = {}) {
  return render(
    <MantineProvider>
      <TimelineView
        mediaFiles={[file(1), file(2)]}
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

describe('TimelineView 滚动加载', () => {
  beforeEach(() => {
    ioCb = null;
    (globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO;
  });

  it('底部哨兵进入视口且 hasMore 时触发 onLoadMore', () => {
    const onLoadMore = vi.fn();
    renderView({ onLoadMore, hasMore: true });
    expect(typeof ioCb).toBe('function'); // 哨兵已注册观察
    ioCb!([{ isIntersecting: true }]);
    expect(onLoadMore).toHaveBeenCalled();
  });

  it('hasMore 为 false 时不触发 onLoadMore', () => {
    const onLoadMore = vi.fn();
    renderView({ onLoadMore, hasMore: false });
    if (ioCb) ioCb([{ isIntersecting: true }]);
    expect(onLoadMore).not.toHaveBeenCalled();
  });
});

describe('TimelineView 卡片级虚拟化与缩略图自适应（FR-141）', () => {
  beforeEach(() => {
    ioCb = null;
    (globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO;
  });

  it('同一日期组按列切成网格行渲染，缩略图请求收敛为列宽像素 sizes', () => {
    // 单组多张：渲染后应出现卡片缩略图，且 sizes 为定宽像素（证明 requestSize 已贯通到卡片）
    renderView({ mediaFiles: [file(1), file(2), file(3), file(4), file(5)] });
    const img = screen.getByAltText('f1.png') as HTMLImageElement;
    // requestSize 贯通后 sizes 形如「<px>px」，而非按视口宽度的静态启发式表达式
    expect(img.getAttribute('sizes')).toMatch(/^\d+px$/);
    // 网格行内卡片均渲染（jsdom 无布局，虚拟化按预估高度全量近似渲染可见行）
    expect(screen.getByAltText('f5.png')).toBeInTheDocument();
  });
});

describe('TimelineView 分组头聚合与组级全选（FR-146）', () => {
  beforeEach(() => {
    (globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO;
  });

  // 带相机/地点的同日媒体
  const withMeta = (id: number, camera: string, location: string): MediaFile => ({
    ...file(id),
    camera,
    location,
  });

  it('分组头展示数量 + 主要设备 + 地点', () => {
    renderView({
      mediaFiles: [
        withMeta(1, 'iPhone 15', '浙江·杭州'),
        withMeta(2, 'iPhone 15', '浙江·杭州'),
        withMeta(3, 'Canon EOS R5', '北京·北京'),
      ],
    });
    // 数量
    expect(screen.getByText('3 项')).toBeInTheDocument();
    // 最常见设备与地点
    expect(screen.getByText('iPhone 15')).toBeInTheDocument();
    expect(screen.getByText('浙江·杭州')).toBeInTheDocument();
  });

  it('无 GPS 时不显示地点', () => {
    renderView({ mediaFiles: [withMeta(1, 'iPhone 15', ''), withMeta(2, 'iPhone 15', '')] });
    expect(screen.getByText('iPhone 15')).toBeInTheDocument();
    // 地点维度无数据，不出现任何「省·市」文本（此处以杭州为反例）
    expect(screen.queryByText('浙江·杭州')).toBeNull();
  });

  it('「选当天全部」一键选中该日全部媒体（day 粒度，FR-146）', async () => {
    const onSelectionChange = vi.fn();
    renderView({
      mediaFiles: [file(1), file(2), file(3)],
      granularity: 'day',
      onSelectionChange,
    });
    await userEvent.click(screen.getByRole('button', { name: '选当天全部' }));
    // 最后一次回调即当天全部 id（升序）
    expect(onSelectionChange).toHaveBeenLastCalledWith([1, 2, 3]);
  });

  it('month 粒度下组级全选文案为「选当月全部」', () => {
    renderView({
      mediaFiles: [file(1), file(2)],
      granularity: 'month',
      onSelectionChange: () => {},
    });
    expect(screen.getByRole('button', { name: '选当月全部' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '选当天全部' })).toBeNull();
  });

  it('未启用选择时不渲染组级全选入口', () => {
    renderView({ mediaFiles: [file(1), file(2)], granularity: 'day' });
    expect(screen.queryByRole('button', { name: '选当天全部' })).toBeNull();
  });
});

describe('TimelineView 空态区分（FR-98）', () => {
  beforeEach(() => {
    (globalThis as unknown as { IntersectionObserver: unknown }).IntersectionObserver = MockIO;
  });

  it('无筛选且 0 结果渲染「空库」空态', () => {
    render(
      <MantineProvider>
        <TimelineView
          mediaFiles={[]}
          loading={false}
          error={null}
          customImageExtensions={{}}
          onErrorClose={() => {}}
          onOpenFile={() => {}}
        />
      </MantineProvider>,
    );
    expect(screen.getByText('暂无媒体文件')).toBeInTheDocument();
    expect(screen.queryByText('没有匹配的媒体')).toBeNull();
  });

  it('有筛选且 0 结果渲染「无匹配结果」空态 + 清除筛选 CTA', async () => {
    const onClearFilter = vi.fn();
    render(
      <MantineProvider>
        <TimelineView
          mediaFiles={[]}
          loading={false}
          error={null}
          customImageExtensions={{}}
          onErrorClose={() => {}}
          onOpenFile={() => {}}
          filtered
          onClearFilter={onClearFilter}
        />
      </MantineProvider>,
    );
    expect(screen.getByText('没有匹配的媒体')).toBeInTheDocument();
    const btn = screen.getByRole('button', { name: '清除筛选' });
    await userEvent.click(btn);
    expect(onClearFilter).toHaveBeenCalledTimes(1);
  });

  it('首屏加载渲染骨架屏', () => {
    const { container } = render(
      <MantineProvider>
        <TimelineView
          mediaFiles={[]}
          loading
          error={null}
          customImageExtensions={{}}
          onErrorClose={() => {}}
          onOpenFile={() => {}}
        />
      </MantineProvider>,
    );
    expect(container.querySelector('.mantine-Skeleton-root')).toBeInTheDocument();
  });
});
