import { test, expect, request as pwRequest, type APIRequestContext, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ensureSetup } from './helpers';

// FR-43 真浏览器端到端：无痕（无认证）打开分享链接 → 视频在线播放 / 图片查看 / 相册网格 / 原文件下载。
// 数据准备经认证 API；查看走全新浏览器上下文（无 cookie = 无痕），验证不跳登录、确实可播放/下载。

const BASE_URL = process.env.TEST_BASE_URL || 'http://localhost:8080';

// 探测 ffmpeg：缺失时整组用例 skip（而非 execFileSync 抛 ENOENT 让 spec 硬红），保证跨机可重复。
const hasFfmpeg = (() => {
  try {
    execFileSync('ffmpeg', ['-version'], { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
})();

let mediaDir = '';
let videoToken = '';
let imageToken = '';
let albumToken = '';
let videoID = 0;
let imageID = 0;
let libraryID = 0;
let albumID = 0;
const videoName = `share-e2e-video-${Date.now()}.mp4`;
const imageName = `share-e2e-image-${Date.now()}.jpg`;

// 用 ffmpeg 生成可被浏览器解码播放的真实样片（H.264 baseline + AAC，moov 前置）。
function generateFixtures(dir: string) {
  execFileSync('ffmpeg', [
    '-y', '-f', 'lavfi', '-i', 'testsrc=duration=2:size=320x240:rate=15',
    '-f', 'lavfi', '-i', 'sine=frequency=440:duration=2',
    '-c:v', 'libx264', '-profile:v', 'baseline', '-pix_fmt', 'yuv420p',
    '-c:a', 'aac', '-movflags', '+faststart', join(dir, videoName),
  ], { stdio: 'ignore' });
  execFileSync('ffmpeg', [
    '-y', '-f', 'lavfi', '-i', 'testsrc=duration=1:size=320x240:rate=1',
    '-frames:v', '1', join(dir, imageName),
  ], { stdio: 'ignore' });
}

// 仅查询本次新建库下的媒体（library_id 过滤 + page_size=100=后端上限，避免历史数据/分页截断）。
async function pollMediaIDs(api: APIRequestContext): Promise<void> {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const res = await api.get(`/api/library/media?library_id=${libraryID}&page_size=100`);
    const data = await res.json();
    const items: Array<{ id: number; file_name: string }> = data.items ?? [];
    const v = items.find(m => m.file_name === videoName);
    const i = items.find(m => m.file_name === imageName);
    if (v && i) {
      videoID = v.id;
      imageID = i.id;
      return;
    }
    await new Promise(r => setTimeout(r, 300));
  }
  throw new Error('等待媒体入库超时');
}

// 预热缩略图：缩略图首请求异步生成返回 202，预热到 200 后浏览器 <img> 才能稳定加载（避免相册网格偶发不出图）。
async function warmThumbnail(api: APIRequestContext, id: number): Promise<void> {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const r = await api.get(`/api/library/thumbnail/${id}`);
    if (r.status() === 200) return;
    await new Promise(res => setTimeout(res, 300));
  }
  // 预热超时不致命：交由用例断言暴露
}

// Windows Chromium 无法可靠等待附件下载完成；浏览器仍需真实触发下载事件，
// 再由同一页面读取相同公开地址并把响应字节写入临时文件，验证可完整下载。
async function assertShareDownloadBytes(page: Page, url: string): Promise<void> {
  const response = await page.evaluate(async (downloadURL) => {
    const res = await fetch(downloadURL);
    return { ok: res.ok, bytes: Array.from(new Uint8Array(await res.arrayBuffer())) };
  }, url);
  expect(response.ok).toBeTruthy();
  expect(response.bytes.length).toBeGreaterThan(0);

  const savedPath = join(tmpdir(), `jianvideo-share-download-${Date.now()}`);
  try {
    writeFileSync(savedPath, Buffer.from(response.bytes));
    expect(statSync(savedPath).size).toBeGreaterThan(0);
  } finally {
    rmSync(savedPath, { force: true });
  }
}

test.describe('FR-43 分享链接真浏览器端到端', () => {
  // 缺 ffmpeg 时跳过（fixture 需可解码样片）；解码类用例较重，串行以减少 CPU 争抢导致的偶发超时
  test.skip(!hasFfmpeg, '未检测到 ffmpeg，跳过真浏览器分享 E2E（fixture 需 ffmpeg 生成可解码样片）');
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async () => {
    // 1) 生成真实样片到临时目录
    mediaDir = mkdtempSync(join(tmpdir(), 'jianvideo-share-e2e-'));
    generateFixtures(mediaDir);

    // 2) 认证 API：初始化建号（FR-109）→ 登录 → 建库 → 扫描 → 取媒体 ID → 建分享（媒体 / 相册）
    const api = await pwRequest.newContext({ baseURL: BASE_URL });
    await ensureSetup(api);
    const login = await api.post('/api/auth/login', { data: { username: 'admin', password: 'admin' } });
    expect(login.ok()).toBeTruthy();

    const createLib = await api.post('/api/library/paths', {
      data: { path: mediaDir.replace(/\\/g, '/'), type: 'local', label: '分享 E2E' },
    });
    expect(createLib.ok()).toBeTruthy();
    libraryID = (await createLib.json()).id;

    const scan = await api.post(`/api/library/scan/${libraryID}`);
    expect(scan.ok()).toBeTruthy();
    await pollMediaIDs(api);
    await warmThumbnail(api, imageID);

    const vShare = await api.post('/api/shares', { data: { resource_type: 'media', resource_id: videoID } });
    expect(vShare.ok()).toBeTruthy();
    videoToken = (await vShare.json()).token;

    const iShare = await api.post('/api/shares', { data: { resource_type: 'media', resource_id: imageID } });
    expect(iShare.ok()).toBeTruthy();
    imageToken = (await iShare.json()).token;

    // 相册分享：建相册、加入图片成员
    const createAlbum = await api.post('/api/albums', { data: { name: '分享相册 E2E', description: '' } });
    expect(createAlbum.ok()).toBeTruthy();
    albumID = (await createAlbum.json()).id;
    const addItem = await api.post(`/api/albums/${albumID}/items`, { data: { media_id: imageID } });
    expect(addItem.ok()).toBeTruthy();
    const aShare = await api.post('/api/shares', { data: { resource_type: 'album', resource_id: albumID } });
    expect(aShare.ok()).toBeTruthy();
    albumToken = (await aShare.json()).token;

    await api.dispose();
  });

  test.afterAll(async () => {
    // 清理：撤销分享、删相册、删库、删临时样片（E2E 专用数据库，清理失败仅告警不致命）
    try {
      const api = await pwRequest.newContext({ baseURL: BASE_URL });
      await api.post('/api/auth/login', { data: { username: 'admin', password: 'admin' } });
      for (const tk of [videoToken, imageToken, albumToken]) {
        if (tk) await api.delete(`/api/shares/${tk}`);
      }
      if (albumID) await api.delete(`/api/albums/${albumID}`);
      if (libraryID) await api.delete(`/api/library/paths/${libraryID}`);
      // 兜底：beforeAll 中途失败可能 libraryID 未赋值但库已建，按 label 反查清理本测试残留
      const listRes = await api.get('/api/library/paths');
      if (listRes.ok()) {
        const paths: Array<{ id: number; label: string }> = (await listRes.json()).items ?? [];
        for (const p of paths) {
          if (p.label === '分享 E2E') await api.delete(`/api/library/paths/${p.id}`);
        }
      }
      await api.dispose();
    } catch (e) {
      console.warn('[share-e2e] afterAll 清理失败（不影响测试结论）:', e);
    }
    if (mediaDir) rmSync(mediaDir, { recursive: true, force: true });
  });

  test('无痕打开视频分享链接：不跳登录、视频确实在线播放、可下载原文件', async ({ page }) => {
    // 全新浏览器上下文默认无 cookie = 无痕/免登
    await page.goto(`/s/${videoToken}`);

    // 停留在公开页、未被任何路由改写到登录页
    await expect(page).toHaveURL(new RegExp(`/s/${videoToken}$`));
    await expect(page).not.toHaveURL(/\/login/);

    const video = page.locator('video');
    await expect(video).toBeVisible();

    // 真正播放：静音后 play()，断言 currentTime 推进（解码出帧）
    await video.evaluate((el: HTMLVideoElement) => { el.muted = true; return el.play(); });
    await expect
      .poll(async () => video.evaluate((el: HTMLVideoElement) => el.currentTime), { timeout: 20000 })
      .toBeGreaterThan(0);
    const readyState = await video.evaluate((el: HTMLVideoElement) => el.readyState);
    expect(readyState).toBeGreaterThanOrEqual(2); // ≥ HAVE_CURRENT_DATA

    // 真实点击必须发出浏览器下载事件；Windows Chromium 对视频附件完成事件不可稳定等待，
    // 故用同一浏览器上下文读取同一公开地址，校验完整响应字节后再落盘到测试临时目录。
    const downloadLink = page.getByRole('link', { name: /下载原文件/ });
    const downloadURL = await downloadLink.getAttribute('href');
    if (!downloadURL) throw new Error('分享下载链接缺失');
    const [download] = await Promise.all([
      page.waitForEvent('download'),
      downloadLink.click(),
    ]);
    expect(download.suggestedFilename()).toContain('.mp4');
    await assertShareDownloadBytes(page, downloadURL);
  });

  test('无痕打开图片分享链接：图片真实渲染、可下载', async ({ page }) => {
    await page.goto(`/s/${imageToken}`);
    await expect(page).toHaveURL(new RegExp(`/s/${imageToken}$`));
    await expect(page).not.toHaveURL(/\/login/);

    const img = page.locator('img').first();
    await expect(img).toBeVisible();
    await expect
      .poll(async () => img.evaluate((el: HTMLImageElement) => el.naturalWidth), { timeout: 20000 })
      .toBeGreaterThan(0);

    const downloadLink = page.getByRole('link', { name: /下载原文件/ });
    const downloadURL = await downloadLink.getAttribute('href');
    if (!downloadURL) throw new Error('分享下载链接缺失');
    const [download] = await Promise.all([
      page.waitForEvent('download'),
      downloadLink.click(),
    ]);
    expect(download.suggestedFilename()).toContain('.jpg');
    await assertShareDownloadBytes(page, downloadURL);
  });

  test('无痕打开相册分享：网格缩略图真出像素、点开成员可查看下载', async ({ page }) => {
    await page.goto(`/s/${albumToken}`);
    await expect(page).toHaveURL(new RegExp(`/s/${albumToken}$`));
    await expect(page).not.toHaveURL(/\/login/);

    // 网格缩略图（/thumbnail 端点）真出像素
    const thumb = page.locator('img').first();
    await expect(thumb).toBeVisible();
    await expect
      .poll(async () => thumb.evaluate((el: HTMLImageElement) => el.naturalWidth), { timeout: 20000 })
      .toBeGreaterThan(0);

    // 点开成员卡片 → Modal 内成员可查看，并提供下载入口（覆盖相册分支 + Modal 渲染）
    await thumb.click();
    const dl = page.getByRole('link', { name: /下载原文件/ });
    await expect(dl).toBeVisible({ timeout: 10000 });
    expect(await dl.getAttribute('href')).toContain(`/api/share/${albumToken}/media/${imageID}/download`);
  });

  test('无痕打开无效分享：显示过期提示且停留在公开页', async ({ page }) => {
    await page.goto('/s/deadbeefdeadbeefdeadbeef');
    await expect(page).toHaveURL(/\/s\/deadbeef/);
    await expect(page).not.toHaveURL(/\/login/);
    await expect(page.getByText(/分享不存在或已过期/)).toBeVisible({ timeout: 10000 });
  });
});
