import { render, screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import { server } from '@/mocks/beforeAll';
import { useState } from 'react';
import PlayPage from './PlayPage';
import { CinemaContext, useCinemaMode } from '@/hooks/cinema-context';

// mock VideoPlayer 组件，避免依赖 mpegts.js
vi.mock('@/components/VideoPlayer', () => ({
  default: (props: {
    url?: string;
    isABR?: boolean;
    streamType?: string;
    initialPosition?: number;
    fill?: boolean;
    descriptor?: { codec: string; path: string; url: string };
  }) => (
    <div
      data-testid="video-player"
      data-url={props.url}
      data-is-abr={String(!!props.isABR)}
      data-stream-type={props.streamType || ''}
      data-initial-position={props.initialPosition ?? ''}
      data-fill={String(!!props.fill)}
      data-desc-codec={props.descriptor?.codec ?? ''}
      data-desc-path={props.descriptor?.path ?? ''}
      data-desc-url={props.descriptor?.url ?? ''}
    />
  ),
}));

function renderPlayPage(route: string) {
  const router = createMemoryRouter([{ path: '/play/:id', element: <PlayPage /> }], {
    initialEntries: [route],
  });
  return render(
    <MantineProvider>
      <RouterProvider router={router} />
    </MantineProvider>,
  );
}

// 影院态探针：把上下文里的 cinema 渲染成 data 属性，便于断言播放页对影院态的切换
function CinemaProbe() {
  const { cinema } = useCinemaMode();
  return <div data-testid="cinema-probe" data-cinema={String(cinema)} />;
}

// 测试用影院 Provider：持有本地态经 CinemaContext 下发，模拟 AppLayout 的下发行为
function TestCinemaProvider({ children }: { children: React.ReactNode }) {
  const [cinema, setCinema] = useState(false);
  return <CinemaContext.Provider value={{ cinema, setCinema }}>{children}</CinemaContext.Provider>;
}

// 带影院 Provider + 探针 + 可切换路由的渲染，用于影院态切入/切出与离开页面恢复的断言。
// 关键：CinemaProvider 与探针置于 router 之上、不随路由卸载，从而能观察 PlayPage 卸载后影院态的恢复。
function renderPlayPageWithCinema(route: string) {
  const router = createMemoryRouter(
    [
      { path: '/play/:id', element: <PlayPage /> },
      { path: '/other', element: <div data-testid="other-page">其它页面</div> },
    ],
    { initialEntries: [route] },
  );
  const result = render(
    <MantineProvider>
      <TestCinemaProvider>
        <CinemaProbe />
        <RouterProvider router={router} />
      </TestCinemaProvider>
    </MantineProvider>,
  );
  // 暴露 router 便于测试触发路由切换，模拟用户离开播放页
  return { ...result, router };
}

describe('PlayPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('渲染加载状态', () => {
    renderPlayPage('/play/1');
    const skeleton = document.querySelector('.mantine-Skeleton-root');
    expect(skeleton).toBeInTheDocument();
  });

  it('无效 ID 显示错误提示', async () => {
    renderPlayPage('/play/0');
    await waitFor(() => {
      expect(screen.getByText('无效的媒体 ID')).toBeInTheDocument();
    });
  });

  it('改显示名：经二次确认后调用 display-name 端点并刷新标题', async () => {
    let displayNameBody: { display_name?: string } | null = null;
    server.use(
      http.get('*/api/library/media/1', () =>
        HttpResponse.json({
          id: 1,
          library_id: 1,
          file_path: 'D:/V/real.mkv',
          file_name: 'real.mkv',
          file_size: 0,
          format: 'mkv',
          video_codec: 'hevc',
          audio_codec: 'aac',
          duration: 0,
          width: 0,
          height: 0,
          bitrate: 0,
          subtitle_tracks: '',
          added_at: '',
          modified_at: '',
        }),
      ),
      http.put('*/api/library/media/1/display-name', async ({ request }) => {
        displayNameBody = (await request.json()) as { display_name?: string };
        return HttpResponse.json({
          id: 1,
          library_id: 1,
          file_path: 'D:/V/real.mkv',
          file_name: 'real.mkv',
          file_size: 0,
          format: 'mkv',
          video_codec: 'hevc',
          audio_codec: 'aac',
          duration: 0,
          width: 0,
          height: 0,
          bitrate: 0,
          subtitle_tracks: '',
          added_at: '',
          modified_at: '',
          display_name: displayNameBody?.display_name,
        });
      }),
    );

    const user = userEvent.setup();
    renderPlayPage('/play/1');

    // 初始展示真实文件名（无显示名时回退）
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'real.mkv' })).toBeInTheDocument(),
    );

    // 打开「更多」菜单后点击「改显示名」二次确认弹窗（FR-85 操作收纳）
    await user.click(screen.getByRole('button', { name: '更多操作' }));
    await user.click(await screen.findByRole('menuitem', { name: '改显示名' }));
    const input = await screen.findByLabelText('显示名');
    await user.clear(input);
    await user.type(input, '我的影片');
    // 二次确认
    await user.click(screen.getByRole('button', { name: '确认修改' }));

    // 调用了 display-name 端点且标题刷新为显示名
    await waitFor(() => expect(displayNameBody).toEqual({ display_name: '我的影片' }));
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '我的影片' })).toBeInTheDocument(),
    );
  });

  it('改文件名：经二次确认后调用 rename 端点（磁盘改名）', async () => {
    let renameBody: { new_name?: string } | null = null;
    server.use(
      http.get('*/api/library/media/1', () =>
        HttpResponse.json({
          id: 1,
          library_id: 1,
          file_path: 'D:/V/old.mkv',
          file_name: 'old.mkv',
          file_size: 0,
          format: 'mkv',
          video_codec: 'hevc',
          audio_codec: 'aac',
          duration: 0,
          width: 0,
          height: 0,
          bitrate: 0,
          subtitle_tracks: '',
          added_at: '',
          modified_at: '',
        }),
      ),
      http.put('*/api/library/media/1/rename', async ({ request }) => {
        renameBody = (await request.json()) as { new_name?: string };
        return HttpResponse.json({
          id: 1,
          library_id: 1,
          file_path: 'D:/V/new.mkv',
          file_name: 'new.mkv',
          file_size: 0,
          format: 'mkv',
          video_codec: 'hevc',
          audio_codec: 'aac',
          duration: 0,
          width: 0,
          height: 0,
          bitrate: 0,
          subtitle_tracks: '',
          added_at: '',
          modified_at: '',
        });
      }),
    );

    const user = userEvent.setup();
    renderPlayPage('/play/1');
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'old.mkv' })).toBeInTheDocument(),
    );

    await user.click(screen.getByRole('button', { name: '更多操作' }));
    await user.click(await screen.findByRole('menuitem', { name: '改文件名' }));
    const input = await screen.findByLabelText('真实文件名');
    await user.clear(input);
    await user.type(input, 'new.mkv');
    await user.click(screen.getByRole('button', { name: '确认修改' }));

    await waitFor(() => expect(renameBody).toEqual({ new_name: 'new.mkv' }));
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'new.mkv' })).toBeInTheDocument(),
    );
  });

  it('master.m3u8 不可用时改用 /api/play/:id/stream', async () => {
    // master.m3u8 探测返回 404 + JSON，content-type 不是 mpegurl，应降级
    server.use(
      http.get('*/api/play/hls/1/master.m3u8', () =>
        HttpResponse.json({ code: 'NOT_FOUND' }, { status: 404 }),
      ),
    );

    renderPlayPage('/play/1');

    const player = await screen.findByTestId('video-player');
    await waitFor(() => {
      const url = player.getAttribute('data-url') || '';
      // URL 是绝对形式，避免 mpegts.js Web Worker 解析相对 URL 失败
      expect(url).toMatch(/\/api\/play\/1\/stream$/);
      expect(url).not.toContain('master.m3u8');
      // 降级路径必须显式关闭 ABR 并切换到原生 mp4 模式
      expect(player.getAttribute('data-is-abr')).toBe('false');
      expect(player.getAttribute('data-stream-type')).toBe('mp4');
    });
  });

  it('把媒体的 last_position 作为续播起点传给播放器（FR-44）', async () => {
    server.use(
      http.get('*/api/library/media/7', () =>
        HttpResponse.json({
          id: 7,
          library_id: 1,
          file_path: 'D:/Videos/a.mp4',
          file_name: 'a.mp4',
          file_size: 1024,
          format: 'mp4',
          video_codec: 'h264',
          audio_codec: 'aac',
          duration: 6600,
          width: 1920,
          height: 1080,
          bitrate: 7000000,
          subtitle_tracks: '',
          added_at: '2025-01-01T12:00:00Z',
          modified_at: '2025-01-01T12:00:00Z',
          last_position: 123.4,
          watched: false,
        }),
      ),
      http.get('*/api/play/hls/7/master.m3u8', () =>
        HttpResponse.json({ code: 'NOT_FOUND' }, { status: 404 }),
      ),
    );

    renderPlayPage('/play/7');

    const player = await screen.findByTestId('video-player');
    await waitFor(() => {
      expect(player.getAttribute('data-initial-position')).toBe('123.4');
    });
  });

  it('协商返回 fMP4 描述符时交自适应播放器（FR-53）', async () => {
    // 浏览器支持 AV1（桩 MediaSource），后端协商出 av1/fMP4
    vi.stubGlobal('MediaSource', { isTypeSupported: () => true } as unknown as typeof MediaSource);
    const captured: { caps?: Record<string, boolean> } = {};
    server.use(
      http.get('*/api/library/media/3', () =>
        HttpResponse.json({
          id: 3,
          library_id: 1,
          file_path: 'D:/V/av1.mkv',
          file_name: 'av1.mkv',
          file_size: 0,
          format: 'mkv',
          video_codec: 'av1',
          audio_codec: 'aac',
          duration: 0,
          width: 1920,
          height: 1080,
          bitrate: 0,
          subtitle_tracks: '',
          added_at: '',
          modified_at: '',
        }),
      ),
      http.post('*/api/play/3/negotiate', async ({ request }) => {
        const reqBody = (await request.json()) as { client_caps?: Record<string, boolean> };
        captured.caps = reqBody.client_caps;
        return HttpResponse.json({
          codec: 'av1',
          path: 'fmp4',
          url: '/api/play/hls/3/index.m3u8',
          mime: 'video/mp4; codecs="av01.0.05M.08"',
          fallback_url: '/api/play/3/stream',
        });
      }),
    );

    renderPlayPage('/play/3');

    const player = await screen.findByTestId('video-player');
    await waitFor(() => {
      // 描述符交给播放器，编码/路径正确
      expect(player.getAttribute('data-desc-codec')).toBe('av1');
      expect(player.getAttribute('data-desc-path')).toBe('fmp4');
      expect(player.getAttribute('data-desc-url')).toMatch(/\/api\/play\/hls\/3\/index\.m3u8$/);
    });
    // 上报了客户端能力
    await waitFor(() => expect(captured.caps).toBeTruthy());
    vi.unstubAllGlobals();
  });

  it('挂载时记录最近查看（PUT /viewed，FR-120）', async () => {
    let viewedId: number | null = null;
    server.use(
      http.put('*/api/library/media/1/viewed', () => {
        viewedId = 1;
        return HttpResponse.json({ ok: true });
      }),
    );

    renderPlayPage('/play/1');

    // 进入播放页即记录最近查看（失败静默、不阻塞播放）
    await waitFor(() => expect(viewedId).toBe(1));
  });

  it('协商失败时回退 H.264：沿用 master 探测 → stream（不报错）', async () => {
    server.use(
      http.get('*/api/library/media/4', () =>
        HttpResponse.json({
          id: 4,
          library_id: 1,
          file_path: 'D:/V/h264.mp4',
          file_name: 'h264.mp4',
          file_size: 0,
          format: 'mp4',
          video_codec: 'h264',
          audio_codec: 'aac',
          duration: 0,
          width: 1280,
          height: 720,
          bitrate: 0,
          subtitle_tracks: '',
          added_at: '',
          modified_at: '',
        }),
      ),
      // 协商端点 500 失败 → 前端不报错，走既有 master 探测路径
      http.post('*/api/play/4/negotiate', () =>
        HttpResponse.json({ code: 'INTERNAL' }, { status: 500 }),
      ),
      // master 不可用 → 降级 stream
      http.get('*/api/play/hls/4/master.m3u8', () =>
        HttpResponse.json({ code: 'NOT_FOUND' }, { status: 404 }),
      ),
    );

    renderPlayPage('/play/4');

    const player = await screen.findByTestId('video-player');
    await waitFor(() => {
      const url = player.getAttribute('data-url') || '';
      expect(url).toMatch(/\/api\/play\/4\/stream$/);
      // 未走 fMP4 描述符
      expect(player.getAttribute('data-desc-path')).toBe('');
    });
  });
});

describe('PlayPage 操作收纳与影院模式（FR-85）', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('头部仅外露返回 + 影院 + 更多，次要操作不平铺为按钮', async () => {
    renderPlayPage('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());

    // 外露：返回、影院模式、更多操作
    expect(screen.getByRole('button', { name: '返回' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '影院模式' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '更多操作' })).toBeInTheDocument();

    // 次要操作不再以独立按钮平铺在头部（已收进「更多」菜单，菜单未展开时不在文档）
    expect(screen.queryByRole('button', { name: '改显示名' })).toBeNull();
    expect(screen.queryByRole('button', { name: '分享' })).toBeNull();
    expect(screen.queryByRole('button', { name: '加入预生成' })).toBeNull();
  });

  it('「更多」菜单收纳全部六个次要操作项', async () => {
    const user = userEvent.setup();
    renderPlayPage('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: '更多操作' }));

    for (const name of ['改显示名', '改文件名', '下载', '分享', '外部播放器', '加入预生成']) {
      expect(await screen.findByRole('menuitem', { name })).toBeInTheDocument();
    }
  });

  it('菜单内点击「分享」触发分享弹窗（FR-43 回归）', async () => {
    const user = userEvent.setup();
    renderPlayPage('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: '更多操作' }));
    await user.click(await screen.findByRole('menuitem', { name: '分享' }));

    // 分享弹窗标题出现，确认菜单项正确触发对应弹窗
    expect(await screen.findByText('分享此媒体')).toBeInTheDocument();
  });

  it('菜单内点击「加入预生成」触发预生成弹窗（FR-77 回归）', async () => {
    const user = userEvent.setup();
    renderPlayPage('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: '更多操作' }));
    await user.click(await screen.findByRole('menuitem', { name: '加入预生成' }));

    // PregenDialog 打开后含「加入预生成队列」标题
    expect(await screen.findByText('加入预生成队列')).toBeInTheDocument();
  });

  it('点「影院模式」切入影院态（cinema=true），再点「退出影院」切出', async () => {
    const user = userEvent.setup();
    renderPlayPageWithCinema('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());

    const probe = screen.getByTestId('cinema-probe');
    expect(probe).toHaveAttribute('data-cinema', 'false');

    // 切入：按钮文案变为「退出影院」，上下文 cinema=true
    await user.click(screen.getByRole('button', { name: '影院模式' }));
    expect(probe).toHaveAttribute('data-cinema', 'true');
    expect(screen.getByRole('button', { name: '退出影院' })).toBeInTheDocument();

    // 切出：恢复非影院态
    await user.click(screen.getByRole('button', { name: '退出影院' }));
    expect(probe).toHaveAttribute('data-cinema', 'false');
    expect(screen.getByRole('button', { name: '影院模式' })).toBeInTheDocument();
  });

  it('离开播放页自动恢复非影院态（导航走后 cinema 回落 false）', async () => {
    const user = userEvent.setup();
    const { router } = renderPlayPageWithCinema('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());

    const probe = screen.getByTestId('cinema-probe');
    await user.click(screen.getByRole('button', { name: '影院模式' }));
    expect(probe).toHaveAttribute('data-cinema', 'true');

    // 离开播放页：PlayPage 卸载，其清理函数应把影院态恢复为 false（Provider 仍挂载、探针可观察）
    await act(async () => {
      await router.navigate('/other');
    });
    await screen.findByTestId('other-page');
    await waitFor(() => expect(probe).toHaveAttribute('data-cinema', 'false'));
  });
});

describe('PlayPage 全屏沉浸布局（FR-103）', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('根容器 100dvh + overflow hidden（铺满视口、不可纵向滚动）', async () => {
    renderPlayPage('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());

    // jsdom 的 CSSOM 不识别 dvh 单位会丢弃该 height 声明（铺满高度下沉到真机验收），
    // 这里断言 jsdom 可识别的「列向 flex + 锁纵向滚动」，并对 height 原始属性串断言 100dvh。
    const root = screen.getByTestId('play-immersive-root');
    expect(root.style.overflow).toBe('hidden');
    expect(root.style.flexDirection).toBe('column');
    expect(root.style.display).toBe('flex');
    expect(root.getAttribute('style')).toContain('height: 100dvh');
  });

  it('视频区以填充模式（fill）渲染播放器，吃满剩余高度', async () => {
    renderPlayPage('/play/1');
    const player = await screen.findByTestId('video-player');
    expect(player.getAttribute('data-fill')).toBe('true');
  });

  it('媒体信息默认不在视频下方文档流（收进抽屉，未打开时不渲染内容）', async () => {
    renderPlayPage('/play/1');
    await screen.findByTestId('video-player');
    // 「媒体信息」标题在抽屉关闭时不出现在文档流
    expect(screen.queryByText('媒体信息')).toBeNull();
  });

  it('「更多」菜单的「详情」项打开媒体信息抽屉', async () => {
    const user = userEvent.setup();
    renderPlayPage('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());

    await user.click(screen.getByRole('button', { name: '更多操作' }));
    await user.click(await screen.findByRole('menuitem', { name: '详情' }));

    // 抽屉打开后展示「媒体信息」与字段标签
    expect(await screen.findByText('媒体信息')).toBeInTheDocument();
    expect(screen.getByText('真实文件名')).toBeInTheDocument();
  });

  it('挂载时给 body 加 play-immersive 类，卸载后移除（协调 AppShell.Main）', async () => {
    const { router } = renderPlayPageWithCinema('/play/1');
    await waitFor(() => expect(screen.getByRole('heading')).toBeInTheDocument());
    expect(document.body.classList.contains('play-immersive')).toBe(true);

    await act(async () => {
      await router.navigate('/other');
    });
    await screen.findByTestId('other-page');
    await waitFor(() => expect(document.body.classList.contains('play-immersive')).toBe(false));
  });
});
