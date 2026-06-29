import { test, expect, request as pwRequest, type APIRequestContext } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BASE_URL, ensureSetup, login } from './helpers';

// FR-127 关键流程端到端：目录浏览交互、设置页保存一项配置、播放页加载。
// 复用 helpers 的 login()/ensureSetup() 与 serviceWorkers:'block'，断言用 role/text/label 避免易朽。

test.use({ serviceWorkers: 'block' });

test.describe('目录浏览页交互', () => {
  test.beforeEach(async ({ page }) => {
    await ensureSetup(page.request);
  });

  test('进入目录浏览并切换视图模式', async ({ page }) => {
    await login(page);
    await page.getByRole('link', { name: '目录' }).click();
    await expect(page).toHaveURL(/\/browse/);
    await expect(page.getByRole('heading', { name: '目录浏览' })).toBeVisible();

    // 视图模式分段控件（FR-121）：切到「列表」并断言选中态生效——不依赖库中是否有媒体
    const viewMode = page.getByLabel('视图模式');
    await expect(viewMode).toBeVisible();
    await viewMode.getByText('列表', { exact: true }).click();
    await expect(viewMode.getByRole('radio', { name: '列表' })).toBeChecked();
  });
});

test.describe('设置页保存配置', () => {
  test.beforeEach(async ({ page }) => {
    await ensureSetup(page.request);
  });

  test('切换调试日志开关并保存成功', async ({ page }) => {
    await login(page);
    // 设置进入控制台页设置 tab（FR-113），旧 /settings 重定向至此
    await page.goto('/system?tab=settings');
    await expect(page.getByRole('heading', { name: '诊断' })).toBeVisible();

    // 翻转调试日志开关（FR-110）。Mantine Switch 的 input 视觉隐藏（由 label 轨道呈现），
    // 故经可见的 label 文案点击，并断言底层 switch 选中态确实翻转。
    const debugSwitch = page.getByRole('switch', { name: '调试日志' });
    const before = await debugSwitch.isChecked();
    await page.getByText('调试日志', { exact: true }).click();
    await expect(debugSwitch).toBeChecked({ checked: !before });

    // 保存并断言成功反馈通知出现
    await page.getByRole('button', { name: '保存设置' }).click();
    await expect(page.getByText('设置已保存')).toBeVisible({ timeout: 10000 });
  });
});

// 探测 ffmpeg：用于生成可入库的真实样片；缺失时退化为「不依赖具体媒体」的播放页断言。
const hasFfmpeg = (() => {
  try {
    execFileSync('ffmpeg', ['-version'], { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
})();

test.describe('播放页加载', () => {
  test.beforeEach(async ({ page }) => {
    await ensureSetup(page.request);
  });

  // 有 ffmpeg：经认证 API 准备一条真实媒体 → 进 /play/:id → 断言进入播放页 + 播放器容器挂载 + 协商发起。
  test('打开真实媒体的播放页：容器挂载且发起编码协商', async ({ page }) => {
    test.skip(!hasFfmpeg, '未检测到 ffmpeg，无法生成可入库样片，跳过真实媒体播放页用例');

    const mediaDir = mkdtempSync(join(tmpdir(), 'jianvideo-play-e2e-'));
    const videoName = `play-e2e-${Date.now()}.mp4`;
    let libraryID = 0;
    let mediaID = 0;
    const api = await pwRequest.newContext({ baseURL: BASE_URL });
    try {
      execFileSync(
        'ffmpeg',
        [
          '-y', '-f', 'lavfi', '-i', 'testsrc=duration=2:size=320x240:rate=15',
          '-c:v', 'libx264', '-profile:v', 'baseline', '-pix_fmt', 'yuv420p',
          '-movflags', '+faststart', join(mediaDir, videoName),
        ],
        { stdio: 'ignore' },
      );

      await api.post('/api/auth/login', { data: { username: 'admin', password: 'admin' } });
      const createLib = await api.post('/api/library/paths', {
        data: { path: mediaDir.replace(/\\/g, '/'), type: 'local', label: '播放 E2E' },
      });
      expect(createLib.ok()).toBeTruthy();
      libraryID = (await createLib.json()).id;
      const scan = await api.post(`/api/library/scan/${libraryID}`);
      expect(scan.ok()).toBeTruthy();
      mediaID = await pollMediaID(api, libraryID, videoName);

      // 捕获播放页发起的编码协商请求（FR-53），与页面导航一并断言
      const negotiate = page.waitForRequest(
        (r) => r.url().includes(`/api/play/${mediaID}/negotiate`) && r.method() === 'POST',
        { timeout: 15000 },
      );
      await login(page);
      await page.goto(`/play/${mediaID}`);

      await expect(page).toHaveURL(new RegExp(`/play/${mediaID}$`));
      await expect(page).not.toHaveURL(/\/login/);
      // 沉浸式播放容器挂载即视为进入播放页（不强求真解码）
      await expect(page.getByTestId('play-immersive-root')).toBeVisible({ timeout: 10000 });
      await negotiate;
    } finally {
      if (libraryID) await api.delete(`/api/library/paths/${libraryID}`);
      await api.dispose();
      rmSync(mediaDir, { recursive: true, force: true });
    }
  });

  // 无 ffmpeg：不依赖具体媒体，断言进入播放路由且发起协商请求（页面挂载即触发，与媒体是否存在无关）。
  test('进入播放路由即发起编码协商', async ({ page }) => {
    test.skip(hasFfmpeg, '已有 ffmpeg，由上方真实媒体用例覆盖更强断言');

    const probeId = 999999;
    const negotiate = page.waitForRequest(
      (r) => r.url().includes(`/api/play/${probeId}/negotiate`) && r.method() === 'POST',
      { timeout: 15000 },
    );
    await login(page);
    await page.goto(`/play/${probeId}`);
    await expect(page).toHaveURL(new RegExp(`/play/${probeId}$`));
    await expect(page).not.toHaveURL(/\/login/);
    await negotiate;
  });
});

// 轮询本次新建库下指定文件名的媒体 ID（page_size=100=后端上限，避免分页截断）。
async function pollMediaID(
  api: APIRequestContext,
  libraryID: number,
  fileName: string,
): Promise<number> {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const res = await api.get(`/api/library/media?library_id=${libraryID}&page_size=100`);
    const items: Array<{ id: number; file_name: string }> = (await res.json()).items ?? [];
    const hit = items.find((m) => m.file_name === fileName);
    if (hit) return hit.id;
    await new Promise((r) => setTimeout(r, 300));
  }
  throw new Error('等待媒体入库超时');
}
