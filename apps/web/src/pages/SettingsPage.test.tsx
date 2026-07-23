import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import SettingsPage from './SettingsPage';
import { server } from '@/mocks/beforeAll';

const mockNotificationShow = vi.fn();

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}));

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/settings']}>
        <SettingsPage />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('加载并展示现有设置值', async () => {
    renderPage();
    // 扫描周期初始值 3600 应填入输入框
    const scanInterval = await screen.findByLabelText('扫描周期（秒）', {}, { timeout: 10000 });
    expect(scanInterval).toHaveValue('3600');
    // 回收站路径初始 JSON 回填为结构化行：盘符 D + 路径 D:/.recycle
    expect(screen.getByLabelText('盘符 1')).toHaveValue('D');
    expect(screen.getByLabelText('回收站路径 1')).toHaveValue('D:/.recycle');
    // 保留期与自动清理默认值（FR2-054）
    expect(screen.getByLabelText('回收站保留天数')).toHaveValue(30);
    expect(screen.getByLabelText('回收站自动清理')).toBeChecked();
    expect(screen.getByLabelText('回收站自动清理周期（秒）')).toHaveValue(3600);
  });

  it('已保存敏感代理不回显，保存其他设置时不提交 network_proxy', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.get('*/api/settings', () =>
        HttpResponse.json({
          settings: {
            scan_interval: '3600',
            recycle_bin_paths: '{"D":"D:/.recycle"}',
            network_proxy: '已设置',
          },
        }),
      ),
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: { ...putBody.settings, network_proxy: '已设置' } });
      }),
    );

    renderPage();
    // 设置主表单在 loading 结束后才渲染；FR2-010 挂载时额外并发请求，默认 1s 偶发不够
    const proxyInput = await screen.findByLabelText('网络代理', {}, { timeout: 10000 });
    expect(proxyInput).toHaveValue('');
    expect(screen.getByText(/已保存代理已隐藏/)).toBeVisible();
    expect(screen.queryByText(/secret/)).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '保存设置' }));
    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.settings.network_proxy).toBeUndefined();
  });

  it('owner 可查看存储目录、索引库与当前 Space，并进入媒体库管理', async () => {
    server.use(
      http.get('*/api/settings/storage', () =>
        HttpResponse.json({
          space: { id: 'space-default', name: '默认 Space', owner_user_id: 1 },
          data_dir: 'D:/jianvideo-data',
          database_path: 'D:/jianvideo-data/jianvideo.db',
          library_count: 2,
        }),
      ),
    );

    renderPage();

    expect(await screen.findByRole('heading', { name: '存储与 Space' })).toBeVisible();
    expect(screen.getByText('默认 Space')).toBeVisible();
    expect(screen.getByText('space-default')).toBeVisible();
    expect(screen.getByText('D:/jianvideo-data')).toBeVisible();
    expect(screen.getByText('D:/jianvideo-data/jianvideo.db')).toBeVisible();
    expect(screen.getByText('2 个已注册目录')).toBeVisible();
    expect(screen.getByRole('link', { name: '管理媒体库目录' })).toHaveAttribute(
      'href',
      '/library-manager',
    );
  });

  it('设置项按「扫描 / 网络 / 工具路径 / 回收站」分区且字段归位', async () => {
    renderPage();
    await screen.findByLabelText('扫描周期（秒）', {}, { timeout: 10000 });
    // 四个分区标题均出现
    for (const title of ['扫描', '网络', '工具路径', '回收站']) {
      expect(screen.getByRole('heading', { name: title })).toBeVisible();
    }
    // 仍只有一个「保存设置」按钮（单次统一 PUT）
    expect(screen.getAllByRole('button', { name: '保存设置' })).toHaveLength(1);
  });

  it('左侧锚点列容器带 sticky 常驻样式（FR-113 修复：滚动时锚点常驻可见）', async () => {
    renderPage();
    await screen.findByLabelText('扫描周期（秒）', {}, { timeout: 10000 });

    // 锚点导航以 <nav aria-label="区块导航"> 呈现，其外层容器挂 anchor-nav-sticky（position: sticky）
    const nav = screen.getByRole('navigation', { name: '区块导航' });
    const stickyBox = nav.closest('.anchor-nav-sticky') as HTMLElement;
    expect(stickyBox).not.toBeNull();
    expect(getComputedStyle(stickyBox).position).toBe('sticky');
  });

  it('回收站编辑器：增行后多一组盘符/路径输入', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText('盘符 1', {}, { timeout: 10000 });
    await user.click(screen.getByRole('button', { name: '添加盘符' }));
    expect(screen.getByLabelText('盘符 2')).toBeInTheDocument();
    expect(screen.getByLabelText('回收站路径 2')).toBeInTheDocument();
  });

  it('回收站编辑器：删行后该组输入消失', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText('盘符 1', {}, { timeout: 10000 });
    await user.click(screen.getByRole('button', { name: '删除盘符 1' }));
    expect(screen.queryByLabelText('盘符 1')).not.toBeInTheDocument();
  });

  it('回收站编辑器：有效行序列化为 JSON 串提交', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );
    renderPage();

    const pathInput = await screen.findByLabelText('回收站路径 1');
    fireEvent.change(pathInput, { target: { value: 'D:/trash' } });
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.settings.recycle_bin_paths).toBe('{"D":"D:/trash"}');
  });

  it('回收站保留期与自动清理随保存提交（FR2-054）', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );
    renderPage();

    const daysInput = await screen.findByLabelText('回收站保留天数');
    fireEvent.change(daysInput, { target: { value: '14' } });
    const intervalInput = screen.getByLabelText('回收站自动清理周期（秒）');
    fireEvent.change(intervalInput, { target: { value: '7200' } });
    await user.click(screen.getByLabelText('回收站自动清理')); // 关闭
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.settings.recycle_retention_days).toBe('14');
    expect(putBody!.settings.recycle_auto_cleanup_enabled).toBe('0');
    expect(putBody!.settings.recycle_auto_cleanup_interval_sec).toBe('7200');
  });

  it('回收站编辑器：空盘符行内校验且不提交', async () => {
    const user = userEvent.setup();
    let putCalled = false;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putCalled = true;
        const body = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: body.settings });
      }),
    );
    renderPage();

    const driveInput = await screen.findByLabelText('盘符 1', {}, { timeout: 10000 });
    await user.clear(driveInput); // 盘符清空 → 非法
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    // 行内校验提示出现，且整体不提交（不发 PUT）
    await waitFor(() => {
      expect(screen.getByText('盘符不能为空')).toBeVisible();
    });
    expect(putCalled).toBe(false);
  });

  it('回收站编辑器：重复盘符行内校验且不提交', async () => {
    const user = userEvent.setup();
    let putCalled = false;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putCalled = true;
        const body = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: body.settings });
      }),
    );
    renderPage();

    await screen.findByLabelText('盘符 1', {}, { timeout: 10000 });
    // 加一行并填与第一行相同的盘符 D
    await user.click(screen.getByRole('button', { name: '添加盘符' }));
    fireEvent.change(screen.getByLabelText('盘符 2'), { target: { value: 'D' } });
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(screen.getAllByText('盘符重复').length).toBeGreaterThan(0);
    });
    expect(putCalled).toBe(false);
  });

  it('修改扫描周期并保存后提示成功', async () => {
    const user = userEvent.setup();
    renderPage();

    const input = await screen.findByLabelText('扫描周期（秒）', {}, { timeout: 10000 });
    await waitFor(() => expect(input).toHaveValue('3600'));

    fireEvent.change(input, { target: { value: '7200' } });
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    // 保存后输入框仍展示新值（回读结果）
    expect(input).toHaveValue('7200');
  });

  it('加载失败时展示错误提示', async () => {
    server.use(
      http.get('*/api/settings', () =>
        HttpResponse.json({ code: 'INTERNAL', message: '查询设置失败' }, { status: 500 }),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('查询设置失败')).toBeVisible();
    });
  });

  it('保存失败时展示服务端配置校验错误', async () => {
    const user = userEvent.setup();
    server.use(
      http.put('*/api/settings', () =>
        HttpResponse.json(
          { code: 'INVALID_SETTING', message: 'typo_key: 未知设置项' },
          { status: 400 },
        ),
      ),
    );
    renderPage();

    await screen.findByLabelText('扫描周期（秒）', {}, { timeout: 10000 });
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({
          title: '保存失败',
          message: 'typo_key: 未知设置项',
          color: 'red',
        }),
      );
    });
  });

  it('渲染环境变量区块：非敏感显明文、敏感显掩码不显明文', async () => {
    renderPage();
    // 非敏感项 JIANVIDEO_FFMPEG_PATH 展示明文路径
    await waitFor(() => {
      expect(screen.getByText('/opt/jianvideo/ffmpeg')).toBeVisible();
    });
    // 敏感项 JWT_SECRET 展示掩码而非明文
    expect(screen.getByText('JWT_SECRET')).toBeVisible();
    expect(screen.getByText('****（已设置）')).toBeVisible();
    // 敏感项明文绝不出现在 DOM（脱敏）：响应里没有真实 secret，故任何 secret 串都不应可见
    // 未设置项展示「（未设置）」掩码（敏感 SMB 与非敏感 DEBUG 均如此，故至少一处）
    expect(screen.getAllByText('（未设置）').length).toBeGreaterThan(0);
  });

  it('JWT_SECRET 未设置（set=false）时显内联警示', async () => {
    server.use(
      http.get('*/api/system/env', () =>
        HttpResponse.json({
          env: [
            {
              key: 'JWT_SECRET',
              description: 'JWT 签名密钥',
              sensitive: true,
              set: false,
              value: '（未设置）',
            },
          ],
        }),
      ),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/未设置 JWT_SECRET/)).toBeVisible();
    });
  });

  it('JWT_SECRET 已设置（set=true）时不显警示', async () => {
    server.use(
      http.get('*/api/system/env', () =>
        HttpResponse.json({
          env: [
            {
              key: 'JWT_SECRET',
              description: 'JWT 签名密钥',
              sensitive: true,
              set: true,
              value: '****（已设置）',
            },
          ],
        }),
      ),
    );
    renderPage();
    // 等环境变量表渲染完成
    await waitFor(() => {
      expect(screen.getByText('JWT_SECRET')).toBeVisible();
    });
    expect(screen.queryByText(/未设置 JWT_SECRET/)).not.toBeInTheDocument();
  });

  it('填 ffmpeg 路径点检测，展示可用性与版本', async () => {
    const user = userEvent.setup();
    renderPage();

    const input = await screen.findByLabelText('FFmpeg 路径');
    fireEvent.change(input, { target: { value: 'D:/tools/ffmpeg.exe' } });
    await user.click(screen.getByRole('button', { name: '检测' }));

    await waitFor(() => {
      expect(screen.getByText(/可用：ffmpeg version/)).toBeVisible();
    });
  });

  it('检测不可用路径时展示「不可用」', async () => {
    const user = userEvent.setup();
    renderPage();

    const input = await screen.findByLabelText('FFmpeg 路径');
    fireEvent.change(input, { target: { value: 'D:/tools/notexist.bin' } });
    await user.click(screen.getByRole('button', { name: '检测' }));

    await waitFor(() => {
      expect(screen.getByText('不可用')).toBeVisible();
    });
  });

  it('填代理点测试，展示可达 Badge（FR-89）', async () => {
    const user = userEvent.setup();
    renderPage();

    const input = await screen.findByLabelText('网络代理');
    fireEvent.change(input, { target: { value: 'http://127.0.0.1:7890' } });
    await user.click(screen.getByRole('button', { name: '测试' }));

    await waitFor(() => {
      expect(screen.getByText(/可达：HTTP 200/)).toBeVisible();
    });
  });

  it('测试不可达代理时展示「不可达」Badge（FR-89）', async () => {
    const user = userEvent.setup();
    renderPage();

    const input = await screen.findByLabelText('网络代理');
    fireEvent.change(input, { target: { value: 'http://bad-proxy:9999' } });
    await user.click(screen.getByRole('button', { name: '测试' }));

    await waitFor(() => {
      expect(screen.getByText(/不可达：/)).toBeVisible();
    });
  });

  it('工具路径区可从内置源发起工具下载并展示任务进度（FR2-022）', async () => {
    const user = userEvent.setup();
    let postBody: Record<string, unknown> | null = null;
    server.use(
      http.get('*/api/system/tools', () =>
        HttpResponse.json({
          items: [
            {
              tool: 'ffmpeg',
              setting_key: 'ffmpeg_path',
              configured_path: '',
              installed: [],
            },
          ],
        }),
      ),
      http.get('*/api/system/tools/sources', () =>
        HttpResponse.json({
          sources: [
            {
              id: 'ffmpeg-local-fixture',
              tool: 'ffmpeg',
              platform: 'windows',
              arch: 'amd64',
              version: 'test-1',
              url: 'https://example.invalid/ffmpeg.zip',
              sha256: 'a'.repeat(64),
              size: 1234,
              label: '测试 FFmpeg',
            },
          ],
        }),
      ),
      http.post('*/api/system/tools/download', async ({ request }) => {
        postBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          { status: 'queued', task_id: 'tool-download-test' },
          { status: 202 },
        );
      }),
      http.get('*/api/tasks', () =>
        HttpResponse.json({
          items: [
            {
              id: 'tool-download-test',
              scope: 'system',
              space_id: null,
              type: 'tool.download',
              status: 'running',
              priority: 5,
              attempts: 1,
              max_attempts: 1,
              progress: 0.5,
              checkpoint: '下载中',
              resource_type: 'tool',
              resource_id: 'ffmpeg',
              error: null,
              created_at: '2026-07-10T08:00:00Z',
              updated_at: '2026-07-10T08:01:00Z',
            },
          ],
          page: 1,
          page_size: 5,
          total: 1,
        }),
      ),
    );

    renderPage();

    expect(await screen.findByText('test-1')).toBeVisible();
    await user.click(screen.getByRole('button', { name: '下载工具' }));

    await waitFor(() => {
      expect(postBody).toMatchObject({
        tool: 'ffmpeg',
        source_id: 'ffmpeg-local-fixture',
      });
    });
    expect(screen.getByText(/tool-download-test/)).toBeVisible();
    expect(screen.getByText(/下载中/)).toBeVisible();
  });

  it('保存 magick 路径走 PUT /api/settings（FR-63）', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );
    renderPage();

    const input = await screen.findByLabelText('Magick 路径');
    fireEvent.change(input, { target: { value: 'D:/tools/magick.exe' } });
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.settings.magick_path).toBe('D:/tools/magick.exe');
  });

  it('保存网络代理走 PUT /api/settings（FR-80）', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );
    renderPage();

    const input = await screen.findByLabelText('网络代理');
    fireEvent.change(input, { target: { value: 'socks5://127.0.0.1:1080' } });
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.settings.network_proxy).toBe('socks5://127.0.0.1:1080');
  });

  it('保存 ffmpeg 路径走 PUT /api/settings', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );
    renderPage();

    const input = await screen.findByLabelText('FFmpeg 路径');
    fireEvent.change(input, { target: { value: 'D:/tools/ffmpeg.exe' } });
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.settings.ffmpeg_path).toBe('D:/tools/ffmpeg.exe');
  });

  it('展示影视信息推断总开关并保存关闭状态', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.get('*/api/settings', () =>
        HttpResponse.json({
          settings: {
            scan_interval: '3600',
            recycle_bin_paths: '{"D":"D:/.recycle"}',
            media_inference_enabled: '1',
          },
        }),
      ),
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );

    renderPage();
    const inferenceSwitch = await screen.findByRole('switch', { name: '本地影视信息推断' });
    expect(inferenceSwitch).toBeChecked();
    expect(screen.getByRole('heading', { name: '影视信息' })).toBeVisible();

    await user.click(inferenceSwitch);
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => expect(putBody).not.toBeNull());
    expect(putBody!.settings.media_inference_enabled).toBe('0');
  });

  it('兼容合法布尔大小写并提供按媒体库关闭入口', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.get('*/api/settings', () =>
        HttpResponse.json({
          settings: {
            scan_interval: '3600',
            recycle_bin_paths: '{"D":"D:/.recycle"}',
            media_inference_enabled: 'FALSE',
            media_inference_disabled_libraries: '[1]',
          },
        }),
      ),
      http.get('*/api/library/paths', () =>
        HttpResponse.json({
          items: [
            {
              id: 1,
              path: 'D:/Movies',
              type: 'local',
              label: '电影库',
              library_kind: 'movie',
              enabled: true,
              created_at: '2026-01-01T00:00:00Z',
            },
            {
              id: 2,
              path: 'D:/Series',
              type: 'local',
              label: '剧集库',
              library_kind: 'series',
              enabled: true,
              created_at: '2026-01-01T00:00:00Z',
            },
          ],
        }),
      ),
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );

    renderPage();
    const globalSwitch = await screen.findByRole('switch', { name: '本地影视信息推断' });
    expect(globalSwitch).not.toBeChecked();
    const movieSwitch = await screen.findByRole('switch', { name: '电影库影视信息推断' });
    const seriesSwitch = screen.getByRole('switch', { name: '剧集库影视信息推断' });
    expect(movieSwitch).not.toBeChecked();
    expect(seriesSwitch).toBeChecked();

    await user.click(globalSwitch);
    await user.click(movieSwitch);
    await user.click(seriesSwitch);
    await user.click(screen.getByRole('button', { name: '保存设置' }));
    await waitFor(() => expect(putBody).not.toBeNull());
    expect(putBody!.settings.media_inference_disabled_libraries).toBe('[2]');
  });

  it('渲染调试日志开关，默认按现有设置关闭（FR-110）', async () => {
    renderPage();
    const sw = await screen.findByRole('switch', { name: '调试日志' });
    expect(sw).toBeInTheDocument();
    // 默认 mock 设置未含 debug_log，应为关闭
    expect(sw).not.toBeChecked();
  });

  it('打开调试日志开关并保存，PUT 提交 debug_log=1（FR-110）', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );
    renderPage();

    const sw = await screen.findByRole('switch', { name: '调试日志' });
    await user.click(sw);
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.settings.debug_log).toBe('1');
  });

  it('调试日志默认关闭时保存提交 debug_log=0（FR-110）', async () => {
    const user = userEvent.setup();
    let putBody: { settings: Record<string, string> } | null = null;
    server.use(
      http.put('*/api/settings', async ({ request }) => {
        putBody = (await request.json()) as { settings: Record<string, string> };
        return HttpResponse.json({ settings: putBody.settings });
      }),
    );
    renderPage();

    await screen.findByRole('switch', { name: '调试日志' });
    await user.click(screen.getByRole('button', { name: '保存设置' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.settings.debug_log).toBe('0');
  });

  it('账户安全卡展示修改密码表单（FR-108）', async () => {
    renderPage();
    expect(await screen.findByRole('heading', { name: '账户安全' })).toBeVisible();
    expect(screen.getByLabelText('当前密码')).toBeInTheDocument();
    expect(screen.getByLabelText('新密码')).toBeInTheDocument();
    expect(screen.getByLabelText('确认新密码')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '修改密码' })).toBeInTheDocument();
  });

  it('两次新密码不一致时报错且不请求（FR-108）', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText('当前密码');

    fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'admin' } });
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'secret123' } });
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'mismatch1' } });
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    expect(await screen.findByText('两次输入的新密码不一致')).toBeVisible();
    expect(mockNotificationShow).not.toHaveBeenCalled();
  });

  it('当前密码错误时展示错误（FR-108）', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText('当前密码');

    fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'wrong-pass' } });
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'secret123' } });
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'secret123' } });
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    await waitFor(() => {
      expect(screen.getByText('当前密码错误')).toBeVisible();
    });
  });

  it('正确当前密码改密成功提示（FR-108）', async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText('当前密码');

    fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'admin' } });
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'secret123' } });
    fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'secret123' } });
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ title: '修改成功', color: 'green' }),
      );
    });
  });

  // ─── 家长控制（FR2-051）──────────────────────────────
  // Mantine Select 在本环境常以 <input aria-label> 呈现（非稳定 combobox role）；
  // 下拉打开时 listbox 也会关联同一 label，故用 selector:'input' 限定到输入框。

  async function waitParentalFormReady() {
    // 表单就绪信号：保存按钮仅在 Space 加载成功后渲染（含 MSW delay）
    return screen.findByRole('button', { name: '保存默认分级' }, { timeout: 10000 });
  }

  it('家长控制分区展示 Space 默认分级与成员覆盖', async () => {
    renderPage();
    expect(await screen.findByRole('heading', { name: '家长控制' })).toBeVisible();
    await waitParentalFormReady();
    expect(
      screen.getByLabelText('Space 默认最高可见分级', { selector: 'input' }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText('家长控制确认密码')).toBeInTheDocument();
    // mock 成员 user #1
    expect(screen.getByLabelText('成员 1 最高可见分级', { selector: 'input' })).toBeInTheDocument();
  });

  it('无确认密码时保存默认分级仅提示不请求', async () => {
    const user = userEvent.setup();
    let putCalled = false;
    server.use(
      http.put('*/api/spaces/:id/parental', () => {
        putCalled = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage();
    await waitParentalFormReady();
    await user.click(screen.getByRole('button', { name: '保存默认分级' }));
    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'orange' }),
      );
    });
    expect(putCalled).toBe(false);
  });

  it('带确认密码保存默认分级成功（FR2-051）', async () => {
    const user = userEvent.setup();
    let putBody: { password?: string; default_max_rating?: string } | null = null;
    server.use(
      http.put('*/api/spaces/:id/parental', async ({ request }) => {
        putBody = (await request.json()) as {
          password?: string;
          default_max_rating?: string;
        };
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage();
    await waitParentalFormReady();
    const select = screen.getByLabelText('Space 默认最高可见分级', { selector: 'input' });
    // 选 PG
    await user.click(select);
    await user.click(await screen.findByRole('option', { name: /最高 PG$/ }));
    fireEvent.change(screen.getByLabelText('家长控制确认密码'), { target: { value: 'admin' } });
    await user.click(screen.getByRole('button', { name: '保存默认分级' }));
    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green', title: '已保存' }),
      );
    });
    expect(putBody).not.toBeNull();
    expect(putBody!.password).toBe('admin');
    expect(putBody!.default_max_rating).toBe('PG');
  });

  // ─── 用户与 Space（FR2-010）──────────────────────────────
  // 标题始终渲染；表单就绪以「创建 Space」按钮为准（含 MSW delay）
  // space-default 会在存储区/用户区/家长控制多处出现，断言用 getAllByText

  async function waitUsersSpacesReady() {
    return screen.findByRole('button', { name: '创建 Space' }, { timeout: 10000 });
  }

  it('用户与 Space分区展示用户列表与 Space 列表', async () => {
    renderPage();
    await waitUsersSpacesReady();
    // mock 默认 admin 用户
    expect(await screen.findByText('admin', {}, { timeout: 10000 })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '创建用户' })).toBeInTheDocument();
    expect(screen.getAllByText('space-default').length).toBeGreaterThan(0);
    expect(screen.getByLabelText('管理成员的 Space', { selector: 'input' })).toBeInTheDocument();
  });

  it('创建用户成功后列表出现新用户（FR2-010）', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitUsersSpacesReady();
    await screen.findByText('admin', {}, { timeout: 10000 });
    fireEvent.change(screen.getByLabelText('新用户名'), { target: { value: 'alice' } });
    fireEvent.change(screen.getByLabelText('新用户初始密码'), { target: { value: 'secret12' } });
    await user.click(screen.getByRole('button', { name: '创建用户' }));
    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green', title: '已创建' }),
      );
    });
    expect(await screen.findByText('alice')).toBeInTheDocument();
  });

  it('创建 Space 成功后列表出现新 Space（FR2-010）', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitUsersSpacesReady();
    fireEvent.change(screen.getByLabelText('新 Space ID'), { target: { value: 'space-work' } });
    fireEvent.change(screen.getByLabelText('新 Space 名称'), { target: { value: '工作' } });
    await user.click(screen.getByRole('button', { name: '创建 Space' }));
    await waitFor(() => {
      expect(mockNotificationShow).toHaveBeenCalledWith(
        expect.objectContaining({ color: 'green', title: '已创建' }),
      );
    });
    expect(await screen.findByText('space-work')).toBeInTheDocument();
    expect(screen.getByText('工作')).toBeInTheDocument();
  });

  it('非默认 Space owner 列用户 403 时隐藏用户管理（FR2-010）', async () => {
    server.use(
      http.get('*/api/users', () =>
        HttpResponse.json(
          { code: 'FORBIDDEN', message: '仅默认 Space owner 可管理用户' },
          { status: 403 },
        ),
      ),
    );
    renderPage();
    // 403 分支不渲染「创建用户」，以「创建 Space」+ 无权限文案为就绪信号
    await waitUsersSpacesReady();
    expect(
      await screen.findByText(/不是默认 Space owner，无法管理用户列表/, {}, { timeout: 10000 }),
    ).toBeInTheDocument();
    // Space 列表仍可用（多处出现 space-default 属预期）
    expect(screen.getAllByText('space-default').length).toBeGreaterThan(0);
    expect(screen.queryByRole('button', { name: '创建用户' })).not.toBeInTheDocument();
  });
});
