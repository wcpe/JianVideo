import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter, useSearchParams } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse, delay } from 'msw';
import TimelinePage from './TimelinePage';
import { server } from '@/mocks/beforeAll';

const mockNavigate = vi.fn();
const mockNotificationShow = vi.fn();

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}));

function renderPage(entry = '/') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[entry]}>
        <TimelinePage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('TimelinePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('时间轴媒体列表按媒体时间组织（sort=media_time，FR-32）', async () => {
    let requestedSort: string | null = null;
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        requestedSort = new URL(request.url).searchParams.get('sort');
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );

    renderPage();

    await waitFor(() => {
      expect(requestedSort).toBe('media_time');
    });
  });

  it('URL ?date= 初始化按当天筛选时间轴（FR-145）', async () => {
    let from: string | null = null;
    let to: string | null = null;
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        const q = new URL(request.url).searchParams;
        from = q.get('time_from');
        to = q.get('time_to');
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );

    renderPage('/timeline?date=2025-06-01');

    await waitFor(() => {
      // 首屏请求即按当天时间下界筛选（当天 00:00）
      expect(from).toBe('2025-06-01');
      // 上界覆盖当天末尾（次日零点，右开等价于含当天），确保整天纳入
      expect(to).toBe('2025-06-02');
    });
  });

  it('按日期分组渲染日期文本，图片与视频卡片均渲染缩略图', async () => {
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({
          items: [
            {
              id: 200,
              library_id: 1,
              file_path: 'D:\\Videos\\Movies\\海报.png',
              file_name: '海报.png',
              file_size: 1200000,
              format: 'png',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 1200,
              height: 800,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2025-01-09T12:00:00Z',
              modified_at: '2025-01-09T12:00:00Z',
            },
            {
              id: 201,
              library_id: 1,
              file_path: 'D:\\Videos\\Movies\\星际穿越.mkv',
              file_name: '星际穿越.mkv',
              file_size: 15_000_000_000,
              format: 'mkv',
              video_codec: 'hevc',
              audio_codec: 'aac',
              duration: 10020,
              width: 3840,
              height: 2160,
              bitrate: 12000000,
              subtitle_tracks: '',
              added_at: '2025-01-01T12:00:00Z',
              modified_at: '2025-01-01T12:00:00Z',
            },
          ],
          total: 2,
          page: 1,
          page_size: 20,
        }),
      ),
    );

    renderPage();

    await screen.findByText('海报.png');
    await screen.findByText('星际穿越.mkv');

    // 两个不同日期各渲染整串日期分组头（FR-138）：YYYY-MM-DD 完整键
    expect(screen.getByText('2025-01-09')).toBeInTheDocument();
    expect(screen.getByText('2025-01-01')).toBeInTheDocument();

    // 图片卡片渲染缩略图指向缩略图 URL（多尺寸：带 size 参数，FR-81）
    const pngThumb = screen.getByRole('img', { name: '海报.png' });
    expect(pngThumb.getAttribute('src')).toContain('/api/library/thumbnail/200');

    // 视频卡片现在同样渲染缩略图
    const mkvThumb = screen.getByRole('img', { name: '星际穿越.mkv' });
    expect(mkvThumb.getAttribute('src')).toContain('/api/library/thumbnail/201');
  });

  it('点击刷新按钮重载首页数据（FR-67）', async () => {
    let requestCount = 0;
    server.use(
      http.get('*/api/library/media', () => {
        requestCount += 1;
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    // 首屏会发起一次请求
    await waitFor(() => {
      expect(requestCount).toBe(1);
    });

    // 点击刷新按钮应再次发起首页请求
    await user.click(screen.getByRole('button', { name: '刷新' }));

    await waitFor(() => {
      expect(requestCount).toBe(2);
    });
  });

  it('切换缩放粒度（日/月/年）改变日期轴分组（FR-32 缺陷复现）', async () => {
    // 两条同年同月不同日的媒体，带 media_time：
    // 日粒度 → 两组（07-01 / 07-20）；月粒度 → 一组（07）；年粒度 → 一组（2023）
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({
          items: [
            {
              id: 301,
              library_id: 1,
              file_path: 'D:\\Photos\\a.jpg',
              file_name: 'a.jpg',
              file_size: 1000,
              format: 'jpg',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 100,
              height: 100,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2020-01-01T00:00:00Z',
              modified_at: '2020-01-01T00:00:00Z',
              media_time: '2023-07-20T10:00:00Z',
            },
            {
              id: 302,
              library_id: 1,
              file_path: 'D:\\Photos\\b.jpg',
              file_name: 'b.jpg',
              file_size: 1000,
              format: 'jpg',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 100,
              height: 100,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2020-01-01T00:00:00Z',
              modified_at: '2020-01-01T00:00:00Z',
              media_time: '2023-07-01T08:00:00Z',
            },
          ],
          total: 2,
          page: 1,
          page_size: 20,
        }),
      ),
    );

    const user = userEvent.setup();
    renderPage();

    // 默认日粒度：两个不同日的整串日期分组头 2023-07-20 / 2023-07-01（FR-138）
    await screen.findByText('2023-07-20');
    expect(screen.getByText('2023-07-01')).toBeInTheDocument();

    // 切到月粒度：合并为单组，分组头为月份键 2023-07，且不再出现日级整串标签
    await user.click(screen.getByRole('radio', { name: '月' }));
    await screen.findByText('2023-07');
    expect(screen.queryByText('2023-07-20')).not.toBeInTheDocument();
    expect(screen.queryByText('2023-07-01')).not.toBeInTheDocument();

    // 切到年粒度：分组头为年份键 2023，且不再出现月级键 2023-07
    // 年份串同时出现在右侧 scrubber 年份刻度层，故按分组头容器作用域取 2023，避免命中刻度
    await user.click(screen.getByRole('radio', { name: '年' }));
    await waitFor(() => {
      const header = document.querySelector('[data-timeline-group-header]') as HTMLElement;
      expect(header).not.toBeNull();
      expect(within(header).getByText('2023')).toBeInTheDocument();
    });
    expect(screen.queryByText('2023-07')).not.toBeInTheDocument();
  });

  it('图片点击打开预览弹窗且不跳转播放页', async () => {
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({
          items: [
            {
              id: 88,
              library_id: 1,
              file_path: 'D:\\Videos\\Movies\\海报.jpg',
              file_name: '海报.jpg',
              file_size: 1200000,
              format: 'jpg',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 1200,
              height: 800,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2025-01-09T12:00:00Z',
              modified_at: '2025-01-09T12:00:00Z',
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
        }),
      ),
    );

    const user = userEvent.setup();
    renderPage();

    // 文件名已移入 pointer-events:none 的卡片底部叠层（FR-99），点击落在缩略图上（行为：点图片打开预览）
    await user.click(await screen.findByAltText('海报.jpg'));

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('img', { name: '海报.jpg' })).toHaveAttribute(
      'src',
      '/api/library/media/88/raw',
    );
    expect(mockNavigate).not.toHaveBeenCalledWith('/play/88');
    expect(screen.queryByText('0:00')).not.toBeInTheDocument();
  });

  it('自定义图片后缀扫描出的文件点击预览而非跳转播放', async () => {
    server.use(
      http.get('*/api/library/paths', () =>
        HttpResponse.json({
          items: [
            {
              id: 1,
              path: 'D:\\Videos\\Movies',
              type: 'local',
              label: '电影',
              enabled: true,
              created_at: '2025-01-09T12:00:00Z',
            },
          ],
        }),
      ),
      http.get('*/api/library/extensions', () =>
        HttpResponse.json({
          items: [
            {
              id: 1,
              library_id: 1,
              extension: 'foo',
              type: 'image',
              is_builtin: 0,
              created_at: '2025-01-09T12:00:00Z',
            },
          ],
        }),
      ),
      http.get('*/api/library/media', async () => {
        await delay(50);
        return HttpResponse.json({
          items: [
            {
              id: 99,
              library_id: 1,
              file_path: 'D:\\Videos\\Movies\\封面.foo',
              file_name: '封面.foo',
              file_size: 1200000,
              format: 'foo',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 1200,
              height: 800,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2025-01-09T12:00:00Z',
              modified_at: '2025-01-09T12:00:00Z',
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
        });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    // 文件名已移入 pointer-events:none 的卡片底部叠层（FR-99），点击落在缩略图上（行为：点图片打开预览）
    await user.click(await screen.findByAltText('封面.foo'));

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('img', { name: '封面.foo' })).toHaveAttribute(
      'src',
      '/api/library/media/99/raw',
    );
    expect(mockNavigate).not.toHaveBeenCalledWith('/play/99');
  });

  // FR-120：缩放含「所有」粒度
  it('缩放控件包含 年/月/日/所有 四档（FR-120）', async () => {
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 }),
      ),
    );

    renderPage();

    for (const name of ['年', '月', '日', '所有']) {
      expect(await screen.findByRole('radio', { name })).toBeInTheDocument();
    }
  });

  it('切到「所有」粒度不按日期分组、密铺单组（FR-120）', async () => {
    // 两条不同日期媒体：日粒度两组（两个日期轴标签），「所有」粒度并入单组
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({
          items: [
            {
              id: 401,
              library_id: 1,
              file_path: 'D:\\Photos\\a.jpg',
              file_name: 'a.jpg',
              file_size: 1000,
              format: 'jpg',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 100,
              height: 100,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2025-03-15T00:00:00Z',
              modified_at: '2025-03-15T00:00:00Z',
            },
            {
              id: 402,
              library_id: 1,
              file_path: 'D:\\Photos\\b.jpg',
              file_name: 'b.jpg',
              file_size: 1000,
              format: 'jpg',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 100,
              height: 100,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2025-01-05T00:00:00Z',
              modified_at: '2025-01-05T00:00:00Z',
            },
          ],
          total: 2,
          page: 1,
          page_size: 20,
        }),
      ),
    );

    const user = userEvent.setup();
    renderPage();

    // 默认日粒度：两个整串日期分组头 2025-03-15 / 2025-01-05（FR-138）
    await screen.findByText('2025-03-15');
    expect(screen.getByText('2025-01-05')).toBeInTheDocument();

    // 切到「所有」：合并为单组（不再出现按日的整串日期标签），两张缩略图仍都在
    await user.click(screen.getByRole('radio', { name: '所有' }));
    await waitFor(() => {
      expect(screen.queryByText('2025-03-15')).not.toBeInTheDocument();
      expect(screen.queryByText('2025-01-05')).not.toBeInTheDocument();
    });
    expect(screen.getByAltText('a.jpg')).toBeInTheDocument();
    expect(screen.getByAltText('b.jpg')).toBeInTheDocument();
  });

  // FR-120：顶部「最近查看」区块（有数据时横向卡片流）
  it('顶部展示「最近查看」回忆区块（有数据时，FR-120）', async () => {
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 }),
      ),
      http.get('*/api/library/recently-viewed', () =>
        HttpResponse.json({
          items: [
            {
              id: 9,
              library_id: 1,
              file_path: 'D:\\Photos\\海报.jpg',
              file_name: '海报.jpg',
              file_size: 1200,
              format: 'jpg',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 1200,
              height: 800,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2025-01-09T12:00:00Z',
              modified_at: '2025-01-09T12:00:00Z',
              last_viewed_at: '2025-06-01T12:00:00Z',
            },
          ],
        }),
      ),
    );

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('最近查看')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /最近查看 海报\.jpg/ })).toBeInTheDocument();
    });
  });

  // FR-120：打开媒体即记录最近查看（handleOpen 触发 PUT /viewed）
  it('点击媒体打开详情时记录最近查看（PUT /viewed，FR-120）', async () => {
    let viewedId: number | null = null;
    server.use(
      http.get('*/api/library/media', () =>
        HttpResponse.json({
          items: [
            {
              id: 88,
              library_id: 1,
              file_path: 'D:\\Photos\\海报.jpg',
              file_name: '海报.jpg',
              file_size: 1200,
              format: 'jpg',
              video_codec: '',
              audio_codec: '',
              duration: 0,
              width: 1200,
              height: 800,
              bitrate: 0,
              subtitle_tracks: '',
              added_at: '2025-01-09T12:00:00Z',
              modified_at: '2025-01-09T12:00:00Z',
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
        }),
      ),
      http.put('*/api/library/media/:id/viewed', ({ params }) => {
        viewedId = Number(params.id);
        return HttpResponse.json({ ok: true });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    // 点击图片打开详情（FR-34）即记录最近查看（FR-120）
    await user.click(await screen.findByAltText('海报.jpg'));

    await waitFor(() => expect(viewedId).toBe(88));
  });
});

describe('TimelinePage 承接页眉全局搜索（FR-132）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  function renderAt(path: string) {
    return render(
      <MantineProvider>
        <MemoryRouter initialEntries={[path]}>
          <TimelinePage />
        </MemoryRouter>
      </MantineProvider>,
    );
  }

  it('URL 带 ?search= 时初始即以该关键词请求媒体（搜索统一到页眉，页内不再有搜索框）', async () => {
    let requestedSearch: string | null = null;
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        requestedSearch = new URL(request.url).searchParams.get('search');
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );

    renderAt('/timeline?search=' + encodeURIComponent('海边'));

    // 首屏请求即携带 search=海边（来自 URL，承接页眉全局搜索 FR-132）
    await waitFor(() => {
      expect(requestedSearch).toBe('海边');
    });
    // 搜索统一到页眉：时间轴页内不再渲染独立搜索框（页眉搜索是唯一入口）
    expect(screen.queryByPlaceholderText(/搜索/)).not.toBeInTheDocument();
  });

  it('页眉搜索改变 URL ?search= 后驱动已挂载时间轴按新词重新请求（联动 FR-132）', async () => {
    const searches: (string | null)[] = [];
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        searches.push(new URL(request.url).searchParams.get('search'));
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );

    render(
      <MantineProvider>
        <MemoryRouter initialEntries={['/timeline?search=' + encodeURIComponent('海边')]}>
          {/* 模拟页眉全局搜索改写 URL ?search=：真实 useSearchParams 改参数（页眉 navigate 的等价效果） */}
          <SearchParamSetter to="森林" />
          <TimelinePage />
        </MemoryRouter>
      </MantineProvider>,
    );

    // 首屏按初始词请求
    await waitFor(() => expect(searches).toContain('海边'));

    // 页眉改写 search → 时间轴据新 URL 参数联动重新请求（不依赖页内搜索框）
    await userEvent.click(screen.getByRole('button', { name: '改搜索' }));
    await waitFor(() => expect(searches).toContain('森林'));
  });
});

// 测试辅助：用真实 useSearchParams 改写 ?search=，模拟页眉全局搜索改写 URL（不受 useNavigate mock 影响）
function SearchParamSetter({ to }: { to: string }) {
  const [, setSearchParams] = useSearchParams();
  return (
    <button type="button" onClick={() => setSearchParams({ search: to })}>
      改搜索
    </button>
  );
}
