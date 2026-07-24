import { render, screen, within, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { MemoryRouter } from 'react-router-dom';
import { describe, it, expect, vi, beforeEach } from 'vitest';

import MediaDetailPanel from './MediaDetailPanel';
import type { MediaFile } from '@/types';

const mockNavigate = vi.fn();
const mockGetMediaMetadata = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockGetMediaTags = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockGetMediaInference = vi.hoisted(() => vi.fn().mockResolvedValue(null));
const mockGetMediaCovers = vi.hoisted(() =>
  vi.fn().mockResolvedValue({ cover: null, candidates: [] }),
);
const mockGenerateMediaCovers = vi.hoisted(() =>
  vi.fn().mockResolvedValue({ status: 'pending', task_id: 1 }),
);
const mockSelectMediaCover = vi.hoisted(() => vi.fn());
const mockGetTask = vi.hoisted(() => vi.fn());
const mockEnqueueMetadataWriteback = vi.hoisted(() =>
  vi.fn().mockResolvedValue({ status: 'pending', task_id: 'wb-1' }),
);
const mockUpdateMediaContentRating = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
const mockListAIResults = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockConfirmAIResult = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
const mockRejectAIResult = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
vi.mock('@/api/library', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/library')>();
  return {
    ...actual,
    getMediaMetadata: mockGetMediaMetadata,
    getMediaTags: mockGetMediaTags,
    getMediaInference: mockGetMediaInference,
    getMediaCovers: mockGetMediaCovers,
    generateMediaCovers: mockGenerateMediaCovers,
    selectMediaCover: mockSelectMediaCover,
    enqueueMetadataWriteback: mockEnqueueMetadataWriteback,
    updateMediaContentRating: mockUpdateMediaContentRating,
  };
});
vi.mock('@/api/ai', () => ({
  listAIResults: mockListAIResults,
  confirmAIResult: mockConfirmAIResult,
  rejectAIResult: mockRejectAIResult,
}));
// 封面任务轮询与写回状态依赖 getTask，必须桩掉以免打真实 /api/tasks
vi.mock('@/api/tasks', () => ({ getTask: mockGetTask }));
const mockNotificationShow = vi.hoisted(() => vi.fn());
vi.mock('@mantine/notifications', () => ({
  notifications: { show: (...args: unknown[]) => mockNotificationShow(...args) },
}));
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return { ...actual, useNavigate: () => mockNavigate };
});

// FR-102：灯箱内视频内嵌 VideoPlayer 直接播放。以桩替身断言其渲染及收到的 URL，
// 避免引入 mpegts.js 真实内核。
vi.mock('@/components/VideoPlayer', () => ({
  default: ({ url, poster }: { url: string; poster?: string }) => (
    <div data-testid="video-player" data-url={url} data-poster={poster} />
  ),
}));

function mediaFile(over: Partial<MediaFile>): MediaFile {
  return {
    id: 1,
    library_id: 1,
    file_path: 'D:/m/a.jpg',
    file_name: 'a.jpg',
    file_size: 1000,
    format: 'jpg',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 800,
    height: 600,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z',
    modified_at: '2025-01-01T00:00:00Z',
    ...over,
  } as MediaFile;
}

function renderPanel(files: MediaFile[], initialIndex: number | null) {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <MediaDetailPanel
          files={files}
          initialIndex={initialIndex}
          onClose={() => {}}
          customImageExtensions={{}}
        />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('MediaDetailPanel 文件详情面板（FR-34）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetMediaMetadata.mockResolvedValue([]);
    mockGetMediaTags.mockResolvedValue([]);
    mockGetMediaInference.mockResolvedValue(null);
    mockGetMediaCovers.mockResolvedValue({ cover: null, candidates: [] });
    mockGenerateMediaCovers.mockResolvedValue({ status: 'pending', task_id: 1 });
    mockGetTask.mockResolvedValue({ status: 'succeeded', error: null });
    mockListAIResults.mockResolvedValue([]);
    mockConfirmAIResult.mockResolvedValue(undefined);
    mockRejectAIResult.mockResolvedValue(undefined);
  });

  it('关闭态（initialIndex=null）不渲染对话框', () => {
    renderPanel([mediaFile({})], null);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('展示 AI 结果并可确认/驳回（FR2-012）', async () => {
    mockListAIResults.mockResolvedValueOnce([
      {
        id: 101,
        space_id: 's1',
        media_id: 7,
        task_type: 'ocr',
        model_id: 'stub-ocr-v1',
        model_version: '1.0.0',
        node_id: 'stub-local',
        batch_id: 'b1',
        payload_json: '{"text":"hello"}',
        manual: false,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
    ]);
    const user = userEvent.setup();
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg', format: 'jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText('ocr')).toBeVisible();
    expect(within(dialog).getByText(/hello/)).toBeVisible();
    await user.click(within(dialog).getByRole('button', { name: '确认' }));
    expect(mockConfirmAIResult).toHaveBeenCalledWith(101);
    await within(dialog).findByText('已确认');
  });

  it('AI 关闭时仅提示不阻断详情（FR2-012）', async () => {
    mockListAIResults.mockRejectedValueOnce({
      response: { data: { code: 'AI_DISABLED', message: 'AI 能力未启用' } },
    });
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg', format: 'jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText('AI 未启用')).toBeVisible();
    expect(within(dialog).getByRole('link', { name: /下载原文件/ })).toBeVisible();
  });

  it('图片：左侧渲染 raw 预览，右侧提供下载原文件入口', async () => {
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg', format: 'jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('img', { name: '风景.jpg' })).toHaveAttribute(
      'src',
      '/api/library/media/7/raw',
    );
    const dl = within(dialog).getByRole('link', { name: /下载原文件/ });
    expect(dl).toHaveAttribute('href', '/api/library/media/7/download');
    // 图片不显示「打开播放」
    expect(within(dialog).queryByRole('button', { name: /打开播放/ })).not.toBeInTheDocument();
  });

  it('视频：内嵌 VideoPlayer 直接播放，不再有「打开播放」按钮（FR-102）', async () => {
    renderPanel([mediaFile({ id: 9, file_name: '电影.mp4', format: 'mp4', duration: 120 })], 0);
    const dialog = await screen.findByRole('dialog');
    // VideoPlayer 经 React.lazy 懒加载，需异步等待挂载
    const player = await within(dialog).findByTestId('video-player');
    expect(player).toBeInTheDocument();
    expect(player.getAttribute('data-url')).toContain('/api/play/9/stream');
    // 去掉中间一步：不再展示「打开播放」按钮，也不跳转
    expect(within(dialog).queryByRole('button', { name: /打开播放/ })).not.toBeInTheDocument();
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  it('视频播放器使用统一缩略图路由作为可选 poster（FR2-059）', async () => {
    renderPanel([mediaFile({ id: 9, file_name: '电影.mp4', format: 'mp4', duration: 120 })], 0);
    const player = await screen.findByTestId('video-player');
    expect(player).toHaveAttribute('data-poster', '/api/library/thumbnail/9');
  });

  it('展示封面候选并支持人工选择（FR2-059）', async () => {
    mockGetMediaCovers.mockResolvedValueOnce({
      cover: {
        media_id: 9,
        space_id: 'space-default',
        selected_asset_id: 101,
        selected_source: 'video_frame',
        selected_timestamp_seconds: 1,
        selected_fingerprint: 'a'.repeat(32),
        manual: false,
        updated_at: '2026-07-12T00:00:00Z',
      },
      candidates: [
        {
          id: 11,
          media_id: 9,
          space_id: 'space-default',
          asset_id: 101,
          source: 'video_frame',
          timestamp_seconds: 1,
          fingerprint: 'a'.repeat(32),
          score: 0.6,
          image_url: '/api/library/media/9/covers/11/image',
          created_at: '2026-07-12T00:00:00Z',
          updated_at: '2026-07-12T00:00:00Z',
        },
        {
          id: 12,
          media_id: 9,
          space_id: 'space-default',
          asset_id: 102,
          source: 'video_frame',
          timestamp_seconds: 5,
          fingerprint: 'b'.repeat(32),
          score: 1,
          image_url: '/api/library/media/9/covers/12/image',
          created_at: '2026-07-12T00:00:00Z',
          updated_at: '2026-07-12T00:00:00Z',
        },
      ],
    });
    mockSelectMediaCover.mockResolvedValueOnce({
      media_id: 9,
      space_id: 'space-default',
      selected_asset_id: 102,
      selected_source: 'video_frame',
      selected_timestamp_seconds: 5,
      selected_fingerprint: 'b'.repeat(32),
      manual: true,
      updated_at: '2026-07-12T00:00:00Z',
    });

    renderPanel([mediaFile({ id: 9, file_name: '电影.mp4', format: 'mp4', duration: 10 })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText('封面')).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: '选择 5.0 秒封面' }));
    expect(mockSelectMediaCover).toHaveBeenCalledWith(9, 12);
    expect(await within(dialog).findByText('人工选择')).toBeInTheDocument();
  });

  it('没有候选时可触发本地封面生成（FR2-059）', async () => {
    renderPanel([mediaFile({ id: 9, file_name: '电影.mp4', format: 'mp4', duration: 10 })], 0);
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(await within(dialog).findByRole('button', { name: '生成封面候选' }));
    expect(mockGenerateMediaCovers).toHaveBeenCalledWith(9, false);
    await vi.waitFor(() => expect(mockGetTask).toHaveBeenCalledWith('1'));
  });

  it('有 EXIF 时展示拍摄信息与外部地图链接（FR-38），无 EXIF 时不展示', async () => {
    const { unmount } = renderPanel(
      [
        mediaFile({
          id: 5,
          file_name: '照片.jpg',
          media_time: '2023-05-01T08:30:00Z',
          media_time_source: 'exif',
          camera: 'Sony A7',
          lens: 'FE 35mm',
          aperture: 'f/1.8',
          shutter: '1/200',
          iso: 100,
          gps_lat: 31.23,
          gps_lon: 121.47,
        }),
      ],
      0,
    );
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/Sony A7/)).toBeInTheDocument();
    expect(within(dialog).getByText(/ISO 100/)).toBeInTheDocument();
    // GPS 外部地图仍作为次要链接保留（FR-106）
    const mapLink = within(dialog).getByRole('link', { name: /在外部地图打开/ });
    expect(mapLink).toHaveAttribute('href', expect.stringContaining('mlat=31.23'));
    expect(mapLink).toHaveAttribute('target', '_blank');
    unmount();

    // 无 EXIF：不渲染相机/地图链接
    renderPanel([mediaFile({ id: 6, file_name: '无exif.jpg' })], 0);
    const dialog2 = await screen.findByRole('dialog');
    expect(within(dialog2).queryByRole('link', { name: /在外部地图打开/ })).not.toBeInTheDocument();
  });

  it('展示文件自带技术元数据基础面板（FR2-030）', async () => {
    mockGetMediaMetadata.mockResolvedValueOnce([
      {
        id: 1,
        media_id: 9,
        space_id: 'space-default',
        source: 'ffprobe',
        tool: 'ffprobe',
        tool_version: '7.1',
        raw_json: '{}',
        normalized_json: JSON.stringify({
          container: { format_name: 'matroska,webm', bitrate: 2048000 },
          video_streams: [
            {
              codec_name: 'h264',
              width: 1920,
              height: 1080,
              frame_rate: '30000/1001',
              color: { space: 'bt709' },
            },
          ],
          audio_streams: [{ codec_name: 'aac', language: 'zh', title: '国语' }],
          subtitle_streams: [{ codec_name: 'subrip', language: 'zh', title: '中文字幕' }],
          tags: { title: '样片标题' },
        }),
        parsed_at: '2026-07-12T00:00:00Z',
        stale: false,
      },
    ]);

    renderPanel([mediaFile({ id: 9, file_name: '样片.mkv', format: 'mkv' })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText('文件自带元数据')).toBeInTheDocument();
    expect(within(dialog).getByText(/matroska,webm/)).toBeInTheDocument();
    expect(within(dialog).getByText(/30000\/1001/)).toBeInTheDocument();
    expect(within(dialog).getByText(/国语/)).toBeInTheDocument();
    expect(within(dialog).getByText(/中文字幕/)).toBeInTheDocument();
    expect(within(dialog).getByText(/bt709/)).toBeInTheDocument();
    expect(within(dialog).getByText(/ffprobe 7.1/)).toBeInTheDocument();
  });

  it('EXIF 光圈/快门/ISO 标准化为标准摄影写法（FR-106）', async () => {
    // 后端裸值：光圈 f/2.8、快门 1/200（无 s）、ISO 数字
    renderPanel(
      [
        mediaFile({
          id: 5,
          file_name: '照片.jpg',
          camera: 'Sony A7',
          aperture: 'f/2.8',
          shutter: '1/200',
          iso: 400,
        }),
      ],
      0,
    );
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('f/2.8')).toBeInTheDocument();
    expect(within(dialog).getByText('1/200s')).toBeInTheDocument();
    expect(within(dialog).getByText('ISO 400')).toBeInTheDocument();
  });

  it('EXIF 区为定宽两列定义列表（FR-106）', async () => {
    renderPanel(
      [mediaFile({ id: 5, file_name: '照片.jpg', camera: 'Sony A7', lens: 'FE 35mm' })],
      0,
    );
    const dialog = await screen.findByRole('dialog');
    // 定宽两列以 <dl>/<dt>/<dd> 定义列表承载，相机/镜头标签为 <dt>
    const dl = within(dialog).getByRole('group', { name: 'EXIF 信息' });
    expect(dl.tagName.toLowerCase()).toBe('dl');
    expect(within(dl).getByText('相机').tagName.toLowerCase()).toBe('dt');
  });

  it('GPS 提供站内地图入口，点击跳 /map 带经纬度（FR-106）', async () => {
    const user = userEvent.setup();
    renderPanel([mediaFile({ id: 5, file_name: '照片.jpg', gps_lat: 31.23, gps_lon: 121.47 })], 0);
    const dialog = await screen.findByRole('dialog');
    const mapBtn = within(dialog).getByRole('button', { name: /在站内地图打开/ });
    await user.click(mapBtn);
    expect(mockNavigate).toHaveBeenCalledWith(
      expect.stringMatching(/^\/map\?.*lat=31\.23.*lon=121\.47/),
    );
  });

  it('工具栏含收藏/分享/旋转/下载（FR-106）', async () => {
    // 传 onToggleFavorite 时显收藏按钮，与分享/旋转/下载一并齐全
    render(
      <MantineProvider>
        <MemoryRouter>
          <MediaDetailPanel
            files={[mediaFile({ id: 7, file_name: '风景.jpg' })]}
            initialIndex={0}
            onClose={() => {}}
            customImageExtensions={{}}
            onToggleFavorite={() => {}}
          />
        </MemoryRouter>
      </MantineProvider>,
    );
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('button', { name: /收藏/ })).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: /分享/ })).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: '向右旋转' })).toBeInTheDocument();
    // 下载为链接形态
    expect(within(dialog).getByRole('link', { name: /下载/ })).toHaveAttribute(
      'href',
      '/api/library/media/7/download',
    );
  });

  it('点收藏触发 onToggleFavorite（FR-106）', async () => {
    const user = userEvent.setup();
    const onToggleFavorite = vi.fn();
    render(
      <MantineProvider>
        <MemoryRouter>
          <MediaDetailPanel
            files={[mediaFile({ id: 7, file_name: '风景.jpg' })]}
            initialIndex={0}
            onClose={() => {}}
            customImageExtensions={{}}
            onToggleFavorite={onToggleFavorite}
          />
        </MemoryRouter>
      </MantineProvider>,
    );
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: /收藏/ }));
    expect(onToggleFavorite).toHaveBeenCalledTimes(1);
    expect(onToggleFavorite.mock.calls[0][0]).toMatchObject({ id: 7 });
  });

  it('信息栏可一键折叠/展开（FR-106）', async () => {
    const user = userEvent.setup();
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    // 默认展开：文件信息可见
    expect(within(dialog).getByText('文件信息')).toBeInTheDocument();
    await user.click(within(dialog).getByRole('button', { name: /折叠信息栏/ }));
    expect(within(dialog).queryByText('文件信息')).not.toBeInTheDocument();
    // 再点展开
    await user.click(within(dialog).getByRole('button', { name: /展开信息栏/ }));
    expect(within(dialog).getByText('文件信息')).toBeInTheDocument();
  });

  it('提供复制路径与复制 GPS 坐标按钮（FR-106）', async () => {
    renderPanel(
      [
        mediaFile({
          id: 5,
          file_name: '照片.jpg',
          file_path: 'D:/m/照片.jpg',
          gps_lat: 31.23,
          gps_lon: 121.47,
        }),
      ],
      0,
    );
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('button', { name: /复制路径/ })).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: /复制坐标/ })).toBeInTheDocument();
  });

  it('上一项/下一项在已加载列表内切换并到端点环绕（FR-105）', async () => {
    const user = userEvent.setup();
    const files = [
      mediaFile({ id: 1, file_name: '第一张.jpg' }),
      mediaFile({ id: 2, file_name: '第二张.jpg' }),
    ];
    renderPanel(files, 0);
    const dialog = await screen.findByRole('dialog');

    // 起始第一项：环绕导航下上一项不再禁用
    expect(within(dialog).getByRole('button', { name: '上一项' })).not.toBeDisabled();
    expect(within(dialog).getByRole('img', { name: '第一张.jpg' })).toBeInTheDocument();

    // 下一项 → 第二张（末项）
    await user.click(within(dialog).getByRole('button', { name: '下一项' }));
    expect(await within(dialog).findByRole('img', { name: '第二张.jpg' })).toHaveAttribute(
      'src',
      '/api/library/media/2/raw',
    );

    // 末项再点下一项 → 环绕回第一张
    await user.click(within(dialog).getByRole('button', { name: '下一项' }));
    expect(await within(dialog).findByRole('img', { name: '第一张.jpg' })).toHaveAttribute(
      'src',
      '/api/library/media/1/raw',
    );
  });

  it('首项点上一项环绕到末项（FR-105）', async () => {
    const user = userEvent.setup();
    const files = [
      mediaFile({ id: 1, file_name: '第一张.jpg' }),
      mediaFile({ id: 2, file_name: '第二张.jpg' }),
      mediaFile({ id: 3, file_name: '第三张.jpg' }),
    ];
    renderPanel(files, 0);
    const dialog = await screen.findByRole('dialog');
    // 首项上一项 → 环绕到末项（第三张）
    await user.click(within(dialog).getByRole('button', { name: '上一项' }));
    expect(await within(dialog).findByRole('img', { name: '第三张.jpg' })).toHaveAttribute(
      'src',
      '/api/library/media/3/raw',
    );
  });

  it('标题区显示位置计数「当前 / 总数」（FR-105）', async () => {
    const files = [
      mediaFile({ id: 1, file_name: 'a.jpg' }),
      mediaFile({ id: 2, file_name: 'b.jpg' }),
      mediaFile({ id: 3, file_name: 'c.jpg' }),
    ];
    renderPanel(files, 1);
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('2 / 3')).toBeInTheDocument();
  });

  it('双击在 1×↔2× 间切换缩放（FR-105）', async () => {
    const user = userEvent.setup();
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    const img = within(dialog).getByRole('img', { name: '风景.jpg' });
    expect(img.style.transform).toContain('scale(1)');
    await user.dblClick(img);
    expect(img.style.transform).toContain('scale(2)');
    await user.dblClick(img);
    expect(img.style.transform).toContain('scale(1)');
  });

  it('放大后拖拽改变平移量 translate（FR-105）', async () => {
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    const img = within(dialog).getByRole('img', { name: '风景.jpg' }) as HTMLElement;
    // 先双击放大到 2×，使拖拽生效
    await userEvent.setup().dblClick(img);
    expect(img.style.transform).toContain('scale(2)');
    // 模拟拖拽：mousedown → mousemove → mouseup
    fireEvent.mouseDown(img, { clientX: 100, clientY: 100 });
    fireEvent.mouseMove(window, { clientX: 160, clientY: 140 });
    fireEvent.mouseUp(window);
    expect(img.style.transform).toMatch(/translate\((?!0px, 0px)/);
  });

  it('右旋按钮改变 transform 含 rotate（FR-105）', async () => {
    const user = userEvent.setup();
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    const img = within(dialog).getByRole('img', { name: '风景.jpg' });
    expect(img.style.transform).toContain('rotate(0deg)');
    await user.click(within(dialog).getByRole('button', { name: '向右旋转' }));
    expect(img.style.transform).toContain('rotate(90deg)');
  });

  it('切换项时触发相邻原图预加载（FR-105）', async () => {
    const created: string[] = [];
    const realCreate = document.createElement.bind(document);
    // 拦截 createElement('img') 记录 src 赋值，验证相邻预取
    const spy = vi
      .spyOn(document, 'createElement')
      .mockImplementation((tag: string, ...rest: unknown[]) => {
        const el = realCreate(tag as 'img', ...(rest as []));
        if (tag === 'img') {
          Object.defineProperty(el, 'src', { set: (v: string) => created.push(v), get: () => '' });
        }
        return el;
      });
    try {
      const files = [
        mediaFile({ id: 1, file_name: 'a.jpg' }),
        mediaFile({ id: 2, file_name: 'b.jpg' }),
        mediaFile({ id: 3, file_name: 'c.jpg' }),
      ];
      renderPanel(files, 1);
      await screen.findByRole('dialog');
      // 中间项应预取前后相邻原图
      expect(created.some((s) => s.includes('/api/library/media/1/raw'))).toBe(true);
      expect(created.some((s) => s.includes('/api/library/media/3/raw'))).toBe(true);
    } finally {
      spy.mockRestore();
    }
  });

  it('展示标签与推断信息，点击标签触发筛选回调（FR2-032）', async () => {
    const user = userEvent.setup();
    mockGetMediaTags.mockResolvedValue([{ id: 9, name: '假期' }]);
    mockGetMediaInference.mockResolvedValue({
      id: 1,
      media_id: 7,
      space_id: 'space-default',
      kind: 'series',
      title: '绝命毒师',
      year: 2008,
      season: 1,
      episode: 2,
      episode_title: '',
      confidence: 0.9,
      source: 'offline_rule',
      rule_version: 'fr2-031-v1',
      manual: false,
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    });
    const onFilterByTag = vi.fn();
    const onClose = vi.fn();
    render(
      <MantineProvider>
        <MemoryRouter>
          <MediaDetailPanel
            files={[mediaFile({ id: 7, file_name: 'S01E02.mkv', format: 'mkv' })]}
            initialIndex={0}
            onClose={onClose}
            customImageExtensions={{}}
            onFilterByTag={onFilterByTag}
          />
        </MemoryRouter>
      </MantineProvider>,
    );
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText('绝命毒师')).toBeInTheDocument();
    expect(within(dialog).getByLabelText('影视推断信息')).toHaveTextContent('S01E02');
    const tagBtn = await within(dialog).findByRole('button', { name: '按标签筛选：假期' });
    await user.click(tagBtn);
    expect(onFilterByTag).toHaveBeenCalledWith(expect.objectContaining({ id: 9, name: '假期' }));
    expect(onClose).toHaveBeenCalled();
  });

  it('元数据加载失败时中文提示且不崩溃（FR2-032）', async () => {
    mockGetMediaMetadata.mockRejectedValue(new Error('network'));
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('加载元数据失败');
    expect(within(dialog).getByText('文件信息')).toBeInTheDocument();
  });

  it('无标签/无推断时展示空态（FR2-032）', async () => {
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText('尚未打标签')).toBeInTheDocument();
    expect(within(dialog).getByText('暂无本地推断片名')).toBeInTheDocument();
  });

  it('图片展示写回入口；二次确认后入队（FR2-033）', async () => {
    const user = userEvent.setup();
    mockGetTask.mockResolvedValue({ status: 'succeeded', error: null });
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg', format: 'jpg' })], 0);
    const dialog = await screen.findByRole('dialog');
    const writebackBtn = await within(dialog).findByRole('button', { name: '写回原文件元数据' });
    await user.click(writebackBtn);

    const confirmDialog = await screen.findByRole('dialog', { name: '确认写回原文件' });
    expect(within(confirmDialog).getByText(/不可逆|快照|备份/)).toBeInTheDocument();
    await user.click(within(confirmDialog).getByRole('button', { name: '确认写回' }));

    expect(mockEnqueueMetadataWriteback).toHaveBeenCalledWith(7, true);
    expect(await within(dialog).findByText(/写回任务 #wb-1/)).toBeInTheDocument();
  });

  it('视频不展示写回按钮，仅提示库内元数据（FR2-033）', async () => {
    renderPanel([mediaFile({ id: 9, file_name: '电影.mp4', format: 'mp4', duration: 120 })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(
      within(dialog).queryByRole('button', { name: '写回原文件元数据' }),
    ).not.toBeInTheDocument();
    expect(await within(dialog).findByText(/视频仅支持库内元数据/)).toBeInTheDocument();
  });

  it('展示内容分级 Badge 与下拉（FR2-051）', async () => {
    renderPanel([mediaFile({ id: 7, file_name: '风景.jpg', content_rating: 'PG-13' })], 0);
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByLabelText(/内容分级 PG-13/)).toBeInTheDocument();
    expect(within(dialog).getByLabelText('内容分级')).toBeInTheDocument();
  });

  it('修改内容分级调用 updateMediaContentRating 并通知父层（FR2-051）', async () => {
    const user = userEvent.setup();
    const onContentRatingChange = vi.fn();
    render(
      <MantineProvider>
        <MemoryRouter>
          <MediaDetailPanel
            files={[mediaFile({ id: 7, file_name: '风景.jpg', content_rating: 'G' })]}
            initialIndex={0}
            onClose={() => {}}
            customImageExtensions={{}}
            onContentRatingChange={onContentRatingChange}
          />
        </MemoryRouter>
      </MantineProvider>,
    );
    const dialog = await screen.findByRole('dialog');
    const select = within(dialog).getByLabelText('内容分级');
    // Mantine Select：点开后选 R
    await user.click(select);
    const option = await screen.findByRole('option', { name: /R · 限制级/ });
    await user.click(option);
    await vi.waitFor(() => {
      expect(mockUpdateMediaContentRating).toHaveBeenCalledWith(7, 'R');
    });
    expect(onContentRatingChange).toHaveBeenCalledWith(7, 'R');
  });
});
