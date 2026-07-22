import { renderHook, waitFor, act } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { useInfiniteMedia } from './useInfiniteMedia';
import { server } from '@/mocks/beforeAll';
import type { MediaFile } from '@/types';

/** 构造一个最小可用的媒体文件，仅 id/file_name 在测试中关心 */
function makeFile(id: number): MediaFile {
  return {
    id,
    library_id: 1,
    file_path: `D:\\Videos\\file-${id}.mp4`,
    file_name: `file-${id}.mp4`,
    file_size: 1000,
    format: 'mp4',
    video_codec: 'h264',
    audio_codec: 'aac',
    duration: 60,
    width: 1920,
    height: 1080,
    bitrate: 1000,
    subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z',
    modified_at: '2025-01-01T00:00:00Z',
  };
}

/**
 * 注册按 page 返回分片的 /api/library/media 处理器。
 * total 固定，items 根据 page 与 page_size 切片，从而模拟真实分页。
 */
function useMediaHandler(allFiles: MediaFile[]) {
  server.use(
    http.get('*/api/library/media', ({ request }) => {
      const url = new URL(request.url);
      const page = Number(url.searchParams.get('page') || '1');
      const pageSize = Number(url.searchParams.get('page_size') || '60');
      const search = url.searchParams.get('search') || '';
      let items = allFiles;
      if (search) items = items.filter((m) => m.file_name.includes(search));
      const total = items.length;
      const start = (page - 1) * pageSize;
      return HttpResponse.json({
        items: items.slice(start, start + pageSize),
        total,
        page,
        page_size: pageSize,
      });
    }),
  );
}

describe('useInfiniteMedia', () => {
  it('首屏加载第一页并计算 hasMore', async () => {
    // 共 5 个文件，pageSize=2 → 第一页 2 个，hasMore=true
    useMediaHandler(Array.from({ length: 5 }, (_, i) => makeFile(i + 1)));

    const { result } = renderHook(() => useInfiniteMedia({ pageSize: 2 }));

    await waitFor(() => expect(result.current.items).toHaveLength(2));
    expect(result.current.total).toBe(5);
    expect(result.current.hasMore).toBe(true);
    expect(result.current.items.map((m) => m.id)).toEqual([1, 2]);
  });

  it('loadMore 追加下一页且按 id 去重', async () => {
    useMediaHandler(Array.from({ length: 5 }, (_, i) => makeFile(i + 1)));

    const { result } = renderHook(() => useInfiniteMedia({ pageSize: 2 }));
    await waitFor(() => expect(result.current.items).toHaveLength(2));

    // 加载第二页 → 累积 4 个
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.items).toHaveLength(4));
    expect(result.current.items.map((m) => m.id)).toEqual([1, 2, 3, 4]);

    // 加载第三页 → 累积 5 个（最后一页只有 1 个），hasMore=false
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.items).toHaveLength(5));
    expect(result.current.hasMore).toBe(false);

    // 已无更多，再次 loadMore 不应改变数量，且无重复 id
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.items).toHaveLength(5);
    expect(new Set(result.current.items.map((m) => m.id)).size).toBe(5);
  });

  it('loadMore 返回重叠数据时按 id 去重不重复追加', async () => {
    // 第一页返回 1,2；第二页故意重叠返回 2,3
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        const page = Number(new URL(request.url).searchParams.get('page') || '1');
        const items = page === 1 ? [makeFile(1), makeFile(2)] : [makeFile(2), makeFile(3)];
        return HttpResponse.json({ items, total: 3, page, page_size: 2 });
      }),
    );

    const { result } = renderHook(() => useInfiniteMedia({ pageSize: 2 }));
    await waitFor(() => expect(result.current.items).toHaveLength(2));

    act(() => result.current.loadMore());
    // 仅追加 id=3，id=2 已存在被去重 → 共 3 个
    await waitFor(() => expect(result.current.items).toHaveLength(3));
    expect(result.current.items.map((m) => m.id)).toEqual([1, 2, 3]);
  });

  it('search 变化时重置并重新从第一页累积', async () => {
    const all = [
      makeFile(1),
      makeFile(2),
      makeFile(3),
      { ...makeFile(99), file_name: 'special-99.mp4' },
    ];
    useMediaHandler(all);

    const { result } = renderHook(() => useInfiniteMedia({ pageSize: 2 }));
    await waitFor(() => expect(result.current.items).toHaveLength(2));

    // 触发搜索，等待防抖（400ms）后只返回匹配项
    act(() => result.current.setSearchInput('special'));
    await waitFor(
      () => {
        expect(result.current.items).toHaveLength(1);
      },
      { timeout: 2000 },
    );
    expect(result.current.items[0].id).toBe(99);
    expect(result.current.total).toBe(1);
    expect(result.current.hasMore).toBe(false);
  });
});
