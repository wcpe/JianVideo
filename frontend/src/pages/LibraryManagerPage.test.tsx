import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse, delay } from 'msw';
import LibraryManagerPage from './LibraryManagerPage';
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

function renderPage() {
  return render(
    <MantineProvider env="test">
      <MemoryRouter initialEntries={['/library-manager']}>
        <LibraryManagerPage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('LibraryManagerPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('渲染存储库管理标题', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('存储库管理')).toBeVisible();
    });
  });

  it('显示添加路径输入框和按钮', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByPlaceholderText('输入目录路径，如 D:\\Videos')).toBeVisible();
      expect(screen.getByRole('button', { name: '添加' })).toBeVisible();
    });
  });

  it('卡片展示该库已索引媒体数量', async () => {
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
              created_at: '2025-01-01T12:00:00Z',
              media_count: 42,
            },
          ],
        }),
      ),
    );

    renderPage();

    const card = (await screen.findByText('电影', {}, { timeout: 10000 })).closest(
      '.mantine-Card-root',
    ) as HTMLElement;
    await waitFor(() => {
      expect(within(card).getByText(/42/)).toBeVisible();
    });
  });

  it('展示来源类型与内容分型，并可更新已有库分型', async () => {
    let payload: { library_kind?: string } | null = null;
    server.use(
      http.get('*/api/library/paths', () =>
        HttpResponse.json({
          items: [
            {
              id: 1,
              path: 'D:\\Videos\\Movies',
              type: 'local',
              library_kind: 'movie',
              label: '电影',
              enabled: true,
              created_at: '2025-01-01T12:00:00Z',
              media_count: 42,
            },
          ],
        }),
      ),
      http.put('*/api/library/paths/:id', async ({ request }) => {
        payload = (await request.json()) as { library_kind?: string };
        return HttpResponse.json({
          id: 1,
          path: 'D:\\Videos\\Movies',
          type: 'local',
          library_kind: payload.library_kind,
          label: '电影',
          enabled: true,
          created_at: '2025-01-01T12:00:00Z',
          media_count: 42,
        });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    const card = (await screen.findByText('电影', {}, { timeout: 10000 })).closest(
      '.mantine-Card-root',
    ) as HTMLElement;
    expect(within(card).getByText('源：本地')).toBeVisible();
    expect(within(card).getByText('内容：电影')).toBeVisible();

    await user.click(within(card).getByLabelText('电影 内容分型', { selector: 'input' }));
    await user.click(await screen.findByRole('option', { name: '分型：剧集' }));

    await waitFor(() => {
      expect(payload).toEqual({ library_kind: 'series' });
    });
  });

  it('新增媒体库时提交选定内容分型', async () => {
    let payload: { library_kind?: string; path?: string } | null = null;
    server.use(
      http.post('*/api/library/paths', async ({ request }) => {
        payload = (await request.json()) as { library_kind?: string; path?: string };
        return HttpResponse.json(
          {
            id: 99,
            path: payload.path,
            type: 'local',
            library_kind: payload.library_kind,
            label: payload.path,
            enabled: true,
            created_at: new Date().toISOString(),
          },
          { status: 201 },
        );
      }),
    );

    const user = userEvent.setup();
    renderPage();

    fireEvent.change(await screen.findByPlaceholderText('输入目录路径，如 D:\\Videos'), {
      target: { value: 'D:\\Videos\\Family' },
    });
    const kindSelect = screen.getByLabelText('新目录内容分型', { selector: 'input' });
    await user.click(kindSelect);
    await user.click(await screen.findByRole('option', { name: '分型：家庭录像' }));
    expect(kindSelect).toHaveValue('分型：家庭录像');
    await user.click(screen.getByRole('button', { name: '添加' }));

    await waitFor(() => {
      expect(payload).toMatchObject({
        library_kind: 'home_video',
        path: 'D:\\Videos\\Family',
      });
    });
  });

  it('不再渲染页内媒体文件列表', async () => {
    renderPage();

    // 等待卡片加载完成
    await screen.findByText('电影', {}, { timeout: 10000 });
    // 媒体文件搜索框与文件名仅属于已移除的媒体列表，不应出现
    expect(screen.queryByPlaceholderText('搜索文件名...')).toBeNull();
    expect(screen.queryByText('星际穿越.mkv')).toBeNull();
  });

  it('点击卡片跳转到对应库的目录视图', async () => {
    server.use(
      http.get('*/api/library/paths', () =>
        HttpResponse.json({
          items: [
            {
              id: 7,
              path: 'D:\\Videos\\Movies',
              type: 'local',
              label: '电影',
              enabled: true,
              created_at: '2025-01-01T12:00:00Z',
              media_count: 3,
            },
          ],
        }),
      ),
    );

    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByText('电影', {}, { timeout: 10000 }));

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith(
        `/browse?library_id=7&path=${encodeURIComponent('D:\\Videos\\Movies')}`,
      );
    });
  });

  it('为指定目录追加自定义后缀时带上 library_id', async () => {
    let payload: { library_id?: number; extension?: string; type?: string } | null = null;
    server.use(
      http.post('*/api/media-types/rules', async ({ request }) => {
        payload = (await request.json()) as { library_id: number; extension: string; type: string };
        return HttpResponse.json(
          {
            id: 99,
            library_id: payload.library_id,
            type: payload.type,
            extension: 'foo',
            label: '',
            description: '',
            enabled: true,
            builtin: false,
            capabilities: ['scan'],
          },
          { status: 201 },
        );
      }),
    );

    const user = userEvent.setup();
    renderPage();

    const movieCard = (await screen.findByText('电影', {}, { timeout: 10000 })).closest(
      '.mantine-Card-root',
    ) as HTMLElement;
    const extensionInput = within(movieCard).getByLabelText('电影 自定义后缀');
    fireEvent.change(extensionInput, { target: { value: '.foo' } });
    expect(extensionInput).toHaveValue('.foo');

    const imageButton = within(movieCard).getByRole('button', { name: '图片' });
    await user.click(imageButton);
    expect(imageButton).toHaveAttribute('data-variant', 'filled');
    await user.click(within(movieCard).getByRole('button', { name: '添加后缀' }));

    await waitFor(() => {
      expect(payload).toEqual({ library_id: 1, extension: '.foo', type: 'image' });
    });
  });

  it('存储库卡片以网格布局渲染，既有交互不回归（FR-65）', async () => {
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
              created_at: '2025-01-01T12:00:00Z',
              media_count: 3,
            },
            {
              id: 2,
              path: 'D:\\Videos\\Anime',
              type: 'local',
              label: '动漫',
              enabled: true,
              created_at: '2025-01-01T12:00:00Z',
              media_count: 5,
            },
          ],
        }),
      ),
    );

    const { container } = renderPage();

    // 两个库卡片都渲染，且容器为 Mantine SimpleGrid（网格布局，非单列 Stack）
    await screen.findByText('电影', {}, { timeout: 10000 });
    await screen.findByText('动漫');
    const grid = container.querySelector('.mantine-SimpleGrid-root');
    expect(grid).not.toBeNull();
    expect(grid?.querySelectorAll('.mantine-Card-root').length).toBe(2);

    // 既有交互不回归：浏览/增量更新/全量扫描/删除按钮仍在
    expect(screen.getAllByRole('button', { name: '浏览' }).length).toBe(2);
    expect(screen.getAllByRole('button', { name: '增量更新' }).length).toBe(2);
    expect(screen.getAllByRole('button', { name: '全量扫描' }).length).toBe(2);
    expect(screen.getByLabelText('删除目录 电影')).toBeVisible();
  });

  it('列出媒体类型说明、能力标签和规则，内置不可删、自定义可删（FR2-025）', async () => {
    server.use(
      http.get('*/api/media-types', () =>
        HttpResponse.json({
          types: [
            {
              type: 'video',
              name: '视频',
              description: '可播放、可转码的视频文件。',
              default_extensions: ['mp4'],
              capabilities: ['scan', 'transcode', 'thumbnail', 'metadata'],
            },
          ],
          rules: [
            {
              id: 'builtin-video-mp4',
              library_id: 1,
              space_id: 'space-default',
              extension: 'mp4',
              type: 'video',
              label: 'MP4 视频',
              description: '常见视频容器。',
              enabled: true,
              builtin: true,
              capabilities: ['scan', 'transcode', 'thumbnail', 'metadata'],
            },
            {
              id: 2,
              library_id: 1,
              space_id: 'space-default',
              extension: 'foo',
              type: 'video',
              label: 'Foo 视频',
              description: '自定义视频封装。',
              enabled: true,
              builtin: false,
              capabilities: ['scan'],
            },
          ],
        }),
      ),
    );

    const user = userEvent.setup();
    renderPage();
    const card = (await screen.findByText('电影', {}, { timeout: 10000 })).closest(
      '.mantine-Card-root',
    ) as HTMLElement;

    // 后缀墙默认折叠（FR-100），先展开摘要行
    await user.click(await within(card).findByLabelText('电影 后缀列表'));

    await waitFor(() => {
      expect(within(card).getByText(/mp4（内置）/)).toBeVisible();
    });
    expect(within(card).getByText('视频')).toBeVisible();
    expect(within(card).getByText('可播放、可转码的视频文件。')).toBeVisible();
    expect(within(card).getByText('可扫描')).toBeVisible();
    expect(within(card).getByText('可转码')).toBeVisible();
    expect(within(card).getByText('MP4 视频')).toBeVisible();
    expect(within(card).getByText('常见视频容器。')).toBeVisible();
    expect(within(card).queryByLabelText('删除后缀 mp4')).toBeNull();
    expect(within(card).getByLabelText('删除后缀 foo')).toBeVisible();
  });

  it('删除自定义规则走 DELETE /api/media-types/rules/:id（FR2-025）', async () => {
    let deletedRuleID = '';
    server.use(
      http.get('*/api/media-types', () =>
        HttpResponse.json({
          types: [
            {
              type: 'video',
              name: '视频',
              description: '可播放、可转码的视频文件。',
              default_extensions: ['mp4'],
              capabilities: ['scan'],
            },
          ],
          rules: [
            {
              id: 2,
              library_id: 1,
              space_id: 'space-default',
              extension: 'foo',
              type: 'video',
              label: 'Foo 视频',
              description: '自定义视频封装。',
              enabled: true,
              builtin: false,
              capabilities: ['scan'],
            },
          ],
        }),
      ),
      http.delete('*/api/media-types/rules/:id', ({ params }) => {
        deletedRuleID = String(params.id);
        return new HttpResponse(null, { status: 204 });
      }),
    );

    const user = userEvent.setup();
    renderPage();
    const card = (await screen.findByText('电影', {}, { timeout: 10000 })).closest(
      '.mantine-Card-root',
    ) as HTMLElement;

    // 后缀墙默认折叠（FR-100），先展开摘要行
    await user.click(await within(card).findByLabelText('电影 后缀列表'));
    await user.click(await within(card).findByLabelText('删除后缀 foo'));

    await waitFor(() => {
      expect(deletedRuleID).toBe('2');
    });
  });

  it('后缀筛选「视频/图片/全部」切换显示对应类型（FR-64）', async () => {
    server.use(
      http.get('*/api/media-types', () =>
        HttpResponse.json({
          types: [
            {
              type: 'video',
              name: '视频',
              description: '可播放、可转码的视频文件。',
              default_extensions: ['mp4'],
              capabilities: ['scan'],
            },
            {
              type: 'image',
              name: '图片',
              description: '可生成缩略图的图片文件。',
              default_extensions: ['jpg'],
              capabilities: ['scan', 'thumbnail', 'metadata'],
            },
          ],
          rules: [
            {
              id: 'builtin-video-mp4',
              library_id: 1,
              space_id: 'space-default',
              extension: 'mp4',
              type: 'video',
              label: 'MP4 视频',
              description: '常见视频容器。',
              enabled: true,
              builtin: true,
              capabilities: ['scan'],
            },
            {
              id: 'builtin-image-jpg',
              library_id: 1,
              space_id: 'space-default',
              extension: 'jpg',
              type: 'image',
              label: 'JPEG 图片',
              description: '常见图片格式。',
              enabled: true,
              builtin: true,
              capabilities: ['scan', 'thumbnail', 'metadata'],
            },
          ],
        }),
      ),
    );

    const user = userEvent.setup();
    renderPage();
    const card = (await screen.findByText('电影', {}, { timeout: 10000 })).closest(
      '.mantine-Card-root',
    ) as HTMLElement;

    // 后缀墙默认折叠（FR-100），先展开摘要行
    await user.click(await within(card).findByLabelText('电影 后缀列表'));

    // 全部：两类都显示
    await waitFor(() => {
      expect(within(card).getByText(/mp4（内置）/)).toBeVisible();
      expect(within(card).getByText(/jpg（内置）/)).toBeVisible();
    });

    // 切到「图片」：仅显示图片后缀
    await user.click(within(card).getByRole('radio', { name: '图片' }));
    await waitFor(() => {
      expect(within(card).queryByText(/mp4（内置）/)).toBeNull();
      expect(within(card).getByText(/jpg（内置）/)).toBeVisible();
    });
  });

  it('内置规则可通过 PUT 禁用（FR2-025）', async () => {
    let payload: { library_id?: number; enabled?: boolean } | null = null;
    server.use(
      http.get('*/api/media-types', () =>
        HttpResponse.json({
          types: [
            {
              type: 'video',
              name: '视频',
              description: '可播放、可转码的视频文件。',
              default_extensions: ['mp4'],
              capabilities: ['scan'],
            },
          ],
          rules: [
            {
              id: 'builtin-video-mp4',
              library_id: 1,
              space_id: 'space-default',
              extension: 'mp4',
              type: 'video',
              label: 'MP4 视频',
              description: '常见视频容器。',
              enabled: true,
              builtin: true,
              capabilities: ['scan'],
            },
          ],
        }),
      ),
      http.put('*/api/media-types/rules/:id', async ({ request }) => {
        payload = (await request.json()) as { library_id?: number; enabled?: boolean };
        return HttpResponse.json({
          id: 'builtin-video-mp4',
          library_id: 1,
          space_id: 'space-default',
          extension: 'mp4',
          type: 'video',
          label: 'MP4 视频',
          description: '常见视频容器。',
          enabled: payload.enabled,
          builtin: true,
          capabilities: ['scan'],
        });
      }),
    );

    const user = userEvent.setup();
    renderPage();
    const card = (await screen.findByText('电影', {}, { timeout: 10000 })).closest(
      '.mantine-Card-root',
    ) as HTMLElement;

    await user.click(await within(card).findByLabelText('电影 后缀列表'));
    await user.click(await within(card).findByLabelText('禁用后缀 mp4'));

    await waitFor(() => {
      expect(payload).toEqual({ library_id: 1, enabled: false });
    });
  });

  it('重复点击添加目录只提交一次', async () => {
    let createCount = 0;
    server.use(
      http.post('*/api/library/paths', async ({ request }) => {
        createCount += 1;
        await delay(100);
        const body = (await request.json()) as { path: string; type: string; label: string };
        return HttpResponse.json(
          {
            id: 99,
            path: body.path,
            type: body.type,
            label: body.label || body.path,
            enabled: true,
            created_at: new Date().toISOString(),
          },
          { status: 201 },
        );
      }),
    );

    const user = userEvent.setup();
    renderPage();

    await user.type(
      await screen.findByPlaceholderText('输入目录路径，如 D:\\Videos'),
      'D:\\Videos\\New',
    );
    const addButton = screen.getByRole('button', { name: '添加' });
    await user.dblClick(addButton);

    await waitFor(() => {
      expect(createCount).toBe(1);
    });
  }, 30000);

  it('扫描中显示可感知的不确定进度状态', async () => {
    server.use(
      http.post('*/api/library/scan/:id', async () => {
        await delay(100);
        return HttpResponse.json({ scanned: 1 });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    await screen.findByText('电影', {}, { timeout: 10000 });
    const scanButton = screen.getAllByRole('button', { name: '增量更新' })[0];
    await user.click(scanButton);

    expect(screen.getByRole('status')).toHaveTextContent('正在扫描');
  });

  it('后缀墙默认折叠，点摘要行可展开并按内置/自定义分组（FR-100）', async () => {
    server.use(
      http.get('*/api/media-types', () =>
        HttpResponse.json({
          types: [
            {
              type: 'video',
              name: '视频',
              description: '可播放、可转码的视频文件。',
              default_extensions: ['mp4'],
              capabilities: ['scan'],
            },
          ],
          rules: [
            {
              id: 'builtin-video-mp4',
              library_id: 1,
              space_id: 'space-default',
              extension: 'mp4',
              type: 'video',
              label: 'MP4 视频',
              description: '常见视频容器。',
              enabled: true,
              builtin: true,
              capabilities: ['scan'],
            },
            {
              id: 2,
              library_id: 1,
              space_id: 'space-default',
              extension: 'foo',
              type: 'video',
              label: 'Foo 视频',
              description: '自定义视频封装。',
              enabled: true,
              builtin: false,
              capabilities: ['scan'],
            },
          ],
        }),
      ),
    );

    const user = userEvent.setup();
    renderPage();
    const card = (await screen.findByText('电影', {}, { timeout: 10000 })).closest(
      '.mantine-Card-root',
    ) as HTMLElement;

    // 摘要行存在且默认折叠：徽标未可见（Collapse 关闭态以 display:none 隐藏，元素仍在 DOM）
    const summary = await within(card).findByLabelText('电影 后缀列表');
    expect(summary).toHaveAttribute('aria-expanded', 'false');
    expect(within(card).queryByText(/mp4（内置）/)).not.toBeVisible();

    // 点击摘要行展开：出现「内置」「自定义」分组标题与对应徽标
    await user.click(summary);
    await waitFor(() => {
      expect(within(card).getByText(/mp4（内置）/)).toBeVisible();
    });
    expect(summary).toHaveAttribute('aria-expanded', 'true');
    expect(within(card).getByText('内置')).toBeVisible();
    expect(within(card).getByText('自定义')).toBeVisible();
    expect(within(card).getByText('foo')).toBeVisible();
  });

  it('提供增量更新与全量扫描两个入口，分别携带对应 mode（FR-27）', async () => {
    let capturedMode = '';
    server.use(
      http.post('*/api/library/scan/:id', ({ request }) => {
        capturedMode = new URL(request.url).searchParams.get('mode') || '';
        return HttpResponse.json({ scanned: 0 });
      }),
    );

    const user = userEvent.setup();
    renderPage();
    await screen.findByText('电影', {}, { timeout: 10000 });

    // 增量更新入口 → mode=incremental
    await user.click(screen.getAllByRole('button', { name: '增量更新' })[0]);
    await waitFor(() => expect(capturedMode).toBe('incremental'));

    // 全量扫描入口 → mode=full
    await user.click(screen.getAllByRole('button', { name: '全量扫描' })[0]);
    await waitFor(() => expect(capturedMode).toBe('full'));
  });
});
