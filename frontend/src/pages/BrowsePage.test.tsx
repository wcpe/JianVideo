import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import BrowsePage from './BrowsePage';
import { server } from '@/mocks/beforeAll';

const mockNavigate = vi.fn();

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: vi.fn(),
  },
}));

function renderPage(initialEntry = '/browse') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <BrowsePage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

// 一个媒体文件工厂，便于在 browse 响应里造文件行
function media(over: Record<string, unknown>) {
  return {
    id: 1,
    library_id: 1,
    file_path: 'D:/Videos/a.mkv',
    file_name: 'a.mkv',
    file_size: 1000,
    format: 'mkv',
    video_codec: 'h264',
    audio_codec: 'aac',
    duration: 0,
    width: 0,
    height: 0,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z',
    modified_at: '2025-01-01T00:00:00Z',
    ...over,
  };
}

describe('BrowsePage 资源管理器布局（FR-121）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('无定位参数时以真实路径树根初始化，列出盘符根（不带 library_id）', async () => {
    let requestedParentPath: string | null = null;
    let requestedLibraryID: string | null = null;
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const url = new URL(request.url);
        const parentPath = url.searchParams.get('parent_path');
        // 仅记录主区根请求（左树也会请求 __root__，此处统一返回同一份根数据）
        if (parentPath === '__root__') {
          requestedParentPath = parentPath;
          requestedLibraryID = url.searchParams.get('library_id');
          return HttpResponse.json({
            breadcrumbs: [{ name: '全部', path: '__root__' }],
            directories: [
              { name: 'D:', path: 'D:' },
              { name: 'E:', path: 'E:' },
            ],
            files: [],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    renderPage();

    // 初始请求走真实路径树根，且不再下发 library_id
    await waitFor(() => {
      expect(requestedParentPath).toBe('__root__');
      expect(requestedLibraryID).toBeNull();
    });
    // 根层列出盘符根（主区 + 左树都会出现，取至少一个即可）
    expect((await screen.findAllByText('D:')).length).toBeGreaterThan(0);
    expect((await screen.findAllByText('E:')).length).toBeGreaterThan(0);
  });

  it('工具栏含上一级 / 刷新 / 视图模式 / 排序', async () => {
    server.use(
      http.get('*/api/library/browse', () =>
        HttpResponse.json({
          breadcrumbs: [{ name: '全部', path: '__root__' }],
          directories: [{ name: 'D:', path: 'D:' }],
          files: [],
        }),
      ),
    );
    renderPage();
    await screen.findAllByText('D:');

    // 工具栏导航按钮
    expect(screen.getByRole('button', { name: '上一级' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '刷新' })).toBeInTheDocument();
    // 视图模式分段控件（详情/列表/大中小）
    expect(screen.getByText('详情')).toBeInTheDocument();
    expect(screen.getByText('大图标')).toBeInTheDocument();
    // 排序下拉接后端
    expect(screen.getByRole('combobox', { name: '排序方式' })).toBeInTheDocument();
    // 根层「上一级」禁用
    expect(screen.getByRole('button', { name: '上一级' })).toBeDisabled();
  });

  it('详情视图渲染名称/修改日期/类型/大小列；目录行类型为「文件夹」', async () => {
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path');
        if (parentPath === 'D:/Videos') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'Videos', path: 'D:/Videos' },
            ],
            directories: [{ name: 'Movies', path: 'D:/Videos/Movies' }],
            files: [media({ id: 1, file_name: '影片.mkv', format: 'mkv', file_size: 2048 })],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    renderPage(`/browse?path=${encodeURIComponent('D:/Videos')}`);

    // 列头
    expect(await screen.findByRole('columnheader', { name: '名称' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: '修改日期' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: '类型' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: '大小' })).toBeInTheDocument();
    // 目录行：类型「文件夹」
    expect(screen.getByText('Movies')).toBeInTheDocument();
    expect(screen.getByText('文件夹')).toBeInTheDocument();
    // 文件行：类型大写 + 大小
    expect(screen.getByText('影片.mkv')).toBeInTheDocument();
    expect(screen.getByText('MKV')).toBeInTheDocument();
  });

  it('地址栏路径段可点跳转到该层', async () => {
    const navPaths: string[] = [];
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path') || '';
        navPaths.push(parentPath);
        if (parentPath === 'D:/Videos/Movies') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'Videos', path: 'D:/Videos' },
              { name: 'Movies', path: 'D:/Videos/Movies' },
            ],
            directories: [],
            files: [media({ id: 1, file_name: 'x.mkv' })],
          });
        }
        if (parentPath === 'D:') {
          return HttpResponse.json({
            breadcrumbs: [{ name: 'D:', path: 'D:' }],
            directories: [{ name: 'Videos', path: 'D:/Videos' }],
            files: [],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    const user = userEvent.setup();
    renderPage(`/browse?path=${encodeURIComponent('D:/Videos/Movies')}`);

    // 地址栏出现可点的中间段「D:」「Videos」（末段 Movies 不可点）
    const dSeg = await screen.findByRole('button', { name: 'D:' });
    await user.click(dSeg);

    // 点「D:」跳到该层，触发 parent_path=D: 的请求
    await waitFor(() => expect(navPaths).toContain('D:'));
  });

  it('双击目录进入下一级', async () => {
    const navPaths: string[] = [];
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path') || '';
        navPaths.push(parentPath);
        if (parentPath === 'D:') {
          return HttpResponse.json({
            breadcrumbs: [{ name: 'D:', path: 'D:' }],
            directories: [{ name: 'Videos', path: 'D:/Videos' }],
            files: [],
          });
        }
        if (parentPath === 'D:/Videos') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'Videos', path: 'D:/Videos' },
            ],
            directories: [],
            files: [media({ id: 9, file_name: '里面.mkv' })],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    const user = userEvent.setup();
    renderPage(`/browse?path=${encodeURIComponent('D:')}`);

    // 主区详情视图里双击目录行 Videos
    const videosRow = await screen.findByText('Videos');
    await user.dblClick(videosRow);

    await waitFor(() => expect(navPaths).toContain('D:/Videos'));
    expect(await screen.findByText('里面.mkv')).toBeInTheDocument();
  });

  it('双击文件打开详情面板（FR-34）', async () => {
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path');
        if (parentPath === 'D:/V') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'V', path: 'D:/V' },
            ],
            directories: [],
            files: [media({ id: 3, file_name: '打开我.mp4', format: 'mp4', duration: 100 })],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    const user = userEvent.setup();
    renderPage(`/browse?path=${encodeURIComponent('D:/V')}`);

    await user.dblClick(await screen.findByText('打开我.mp4'));
    // 详情面板（Modal）打开后含位置计数「1 / 1」
    expect(await screen.findByText('1 / 1')).toBeInTheDocument();
  });

  it('切换视图模式到大图标渲染缩略图网格', async () => {
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path');
        if (parentPath === 'D:/V') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'V', path: 'D:/V' },
            ],
            directories: [],
            files: [media({ id: 7, file_name: '图.jpg', format: 'jpg' })],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    const user = userEvent.setup();
    renderPage(`/browse?path=${encodeURIComponent('D:/V')}`);
    await screen.findByText('图.jpg');

    // 切到「大图标」→ 渲染缩略图 <img>
    await user.click(screen.getByText('大图标'));
    await waitFor(() => {
      expect(screen.getByRole('img', { name: '图.jpg' }).getAttribute('src')).toContain(
        '/api/library/thumbnail/7',
      );
    });
  });

  it('排序经后端 sort 参数生效', async () => {
    const sorts: string[] = [];
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const url = new URL(request.url);
        const parentPath = url.searchParams.get('parent_path');
        if (parentPath === 'D:/V') {
          sorts.push(url.searchParams.get('sort') || '');
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'V', path: 'D:/V' },
            ],
            directories: [],
            files: [media({ id: 1, file_name: 'a.mkv' })],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    const user = userEvent.setup();
    renderPage(`/browse?path=${encodeURIComponent('D:/V')}`);
    await screen.findByText('a.mkv');

    // 改排序为「大小」→ 重新以 sort=size 请求当前目录
    await user.selectOptions(screen.getByRole('combobox', { name: '排序方式' }), 'size');
    await waitFor(() => expect(sorts).toContain('size'));
  });

  it('状态栏展示项目数', async () => {
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path');
        if (parentPath === 'D:/V') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'V', path: 'D:/V' },
            ],
            directories: [{ name: 'sub', path: 'D:/V/sub' }],
            files: [media({ id: 1, file_name: 'a.mkv' }), media({ id: 2, file_name: 'b.mkv' })],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    renderPage(`/browse?path=${encodeURIComponent('D:/V')}`);
    await screen.findByText('a.mkv');
    // 1 目录 + 2 文件 = 3 个项目
    expect(await screen.findByText('3 个项目')).toBeInTheDocument();
  });

  it('选中文件后工具栏批量动作可用、状态栏显示选中数（FR-69 不回归）', async () => {
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path');
        if (parentPath === 'D:/V') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'V', path: 'D:/V' },
            ],
            directories: [],
            files: [media({ id: 1, file_name: 'a.mkv', file_size: 1000 })],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
    );

    const user = userEvent.setup();
    renderPage(`/browse?path=${encodeURIComponent('D:/V')}`);
    const row = await screen.findByText('a.mkv');

    // 未选中时工具栏删除按钮禁用
    expect(screen.getByRole('button', { name: '删除' })).toBeDisabled();

    // 单击选中该文件 → 删除按钮可用、状态栏出现「已选 1 项」
    await user.click(row);
    await waitFor(() => expect(screen.getByRole('button', { name: '删除' })).toBeEnabled());
    expect(screen.getByText(/已选 1 项/)).toBeInTheDocument();
  });
});

describe('BrowsePage 当前目录搜索/筛选（FR-36，消费 FR-35）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('目录下搜索按当前路径查媒体接口，透传表达式与类型', async () => {
    let mediaPath: string | null = null;
    let mediaSearch: string | null = null;
    let mediaType: string | null = null;
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path');
        if (parentPath === 'D:/Videos/Movies') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'Videos', path: 'D:/Videos' },
              { name: 'Movies', path: 'D:/Videos/Movies' },
            ],
            directories: [],
            files: [],
          });
        }
        return HttpResponse.json({ breadcrumbs: [], directories: [], files: [] });
      }),
      http.get('*/api/library/media', ({ request }) => {
        const url = new URL(request.url);
        mediaPath = url.searchParams.get('path');
        mediaSearch = url.searchParams.get('search');
        mediaType = url.searchParams.get('type');
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 100 });
      }),
    );

    renderPage(`/browse?path=${encodeURIComponent('D:/Videos/Movies')}`);
    // 等目录初始化（末段 Movies 出现在地址栏）
    await screen.findByText('Movies');

    // 输入表达式搜索 → 防抖后按当前目录路径查媒体接口
    await userEvent.type(screen.getByLabelText('在当前目录下搜索'), 'ext:jpg 海报');
    await waitFor(
      () => {
        expect(mediaSearch).toBe('ext:jpg 海报');
        expect(mediaPath).toBe('D:/Videos/Movies');
      },
      { timeout: 2000 },
    );

    // 选择类型「图片」→ 透传 type=image
    await userEvent.click(screen.getByRole('radio', { name: '图片' }));
    await waitFor(() => expect(mediaType).toBe('image'), { timeout: 2000 });
  });

  it('目录加载错误可见', async () => {
    server.use(
      http.get('*/api/library/browse', () =>
        HttpResponse.json({ message: '目录无法访问' }, { status: 500 }),
      ),
    );

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('目录无法访问')).toBeVisible();
    });
  });
});

describe('BrowsePage 移动端折叠（FR-86）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.use(
      http.get('*/api/library/browse', ({ request }) => {
        const parentPath = new URL(request.url).searchParams.get('parent_path');
        if (parentPath === 'D:/Videos/Movies') {
          return HttpResponse.json({
            breadcrumbs: [
              { name: 'D:', path: 'D:' },
              { name: 'Videos', path: 'D:/Videos' },
              { name: 'Movies', path: 'D:/Videos/Movies' },
            ],
            directories: [],
            files: [],
          });
        }
        return HttpResponse.json({
          breadcrumbs: [{ name: '全部', path: '__root__' }],
          directories: [],
          files: [],
        });
      }),
      http.get('*/api/library/media', () =>
        HttpResponse.json({ items: [], total: 0, page: 1, page_size: 100 }),
      ),
    );
  });

  it('提供「筛选」与「目录树」抽屉触发器，搜索框常驻在抽屉之外', async () => {
    renderPage(`/browse?path=${encodeURIComponent('D:/Videos/Movies')}`);
    await screen.findByText('Movies');

    expect(screen.getByLabelText('在当前目录下搜索')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '筛选' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '目录树' })).toBeInTheDocument();
    // 默认抽屉关闭，无弹层
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('点「筛选」展开抽屉后，抽屉内选择类型「图片」仍透传 type=image', async () => {
    let mediaType: string | null = null;
    server.use(
      http.get('*/api/library/media', ({ request }) => {
        mediaType = new URL(request.url).searchParams.get('type');
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 100 });
      }),
    );

    const user = userEvent.setup();
    renderPage(`/browse?path=${encodeURIComponent('D:/Videos/Movies')}`);
    await screen.findByText('Movies');

    // 打开筛选抽屉，在抽屉内选择类型「图片」
    await user.click(screen.getByRole('button', { name: '筛选' }));
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('radio', { name: '图片' }));

    await waitFor(() => expect(mediaType).toBe('image'), { timeout: 2000 });
  });
});
