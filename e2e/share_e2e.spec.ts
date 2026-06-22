import { test, expect, request as pwRequest, type APIRequestContext } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// FR-43 真浏览器端到端：无痕（无认证）打开分享链接 → 视频在线播放 / 图片查看 / 原文件下载。
// 数据准备经认证 API；查看走全新浏览器上下文（无 cookie = 无痕），验证不跳登录、确实可播放/下载。

const BASE_URL = process.env.TEST_BASE_URL || 'http://localhost:8080';

let mediaDir = '';
let videoToken = '';
let imageToken = '';
let videoID = 0;
let imageID = 0;
let libraryID = 0;
const videoName = `share-e2e-video-${Date.now()}.mp4`;
const imageName = `share-e2e-image-${Date.now()}.jpg`;

// 用 ffmpeg 生成可被浏览器解码播放的真实样片（H.264 baseline + AAC，moov 前置）。
function generateFixtures(dir: string) {
  const videoPath = join(dir, videoName);
  const imagePath = join(dir, imageName);
  execFileSync('ffmpeg', [
    '-y', '-f', 'lavfi', '-i', 'testsrc=duration=2:size=320x240:rate=15',
    '-f', 'lavfi', '-i', 'sine=frequency=440:duration=2',
    '-c:v', 'libx264', '-profile:v', 'baseline', '-pix_fmt', 'yuv420p',
    '-c:a', 'aac', '-movflags', '+faststart', videoPath,
  ], { stdio: 'ignore' });
  execFileSync('ffmpeg', [
    '-y', '-f', 'lavfi', '-i', 'testsrc=duration=1:size=320x240:rate=1',
    '-frames:v', '1', imagePath,
  ], { stdio: 'ignore' });
}

// 仅查询本次新建库下的媒体（避免持久化开发库历史数据 + 分页截断导致找不到样片）。
async function pollMediaIDs(api: APIRequestContext): Promise<void> {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const res = await api.get(`/api/library/media?library_id=${libraryID}&page_size=500`);
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

test.beforeAll(async () => {
  // 1) 生成真实样片到临时目录
  mediaDir = mkdtempSync(join(tmpdir(), 'jianvideo-share-e2e-'));
  generateFixtures(mediaDir);

  // 2) 认证 API 上下文：登录 → 建库 → 扫描 → 取媒体 ID → 建分享
  const api = await pwRequest.newContext({ baseURL: BASE_URL });
  const login = await api.post('/api/auth/login', { data: { username: 'admin', password: 'admin' } });
  expect(login.ok()).toBeTruthy();

  const createLib = await api.post('/api/library/paths', {
    data: { path: mediaDir.replace(/\\/g, '/'), type: 'local', label: '分享 E2E' },
  });
  expect(createLib.ok()).toBeTruthy();
  libraryID = (await createLib.json()).id;

  await api.post(`/api/library/scan/${libraryID}`);
  await pollMediaIDs(api);

  const vShare = await api.post('/api/shares', { data: { resource_type: 'media', resource_id: videoID } });
  expect(vShare.ok()).toBeTruthy();
  videoToken = (await vShare.json()).token;

  const iShare = await api.post('/api/shares', { data: { resource_type: 'media', resource_id: imageID } });
  expect(iShare.ok()).toBeTruthy();
  imageToken = (await iShare.json()).token;

  // 撤销/删除清理交给 afterAll；此处保留 api 上下文释放
  await api.dispose();
});

test.afterAll(async () => {
  // 清理：撤销分享、删除库、删临时样片，避免污染开发库
  try {
    const api = await pwRequest.newContext({ baseURL: BASE_URL });
    await api.post('/api/auth/login', { data: { username: 'admin', password: 'admin' } });
    if (videoToken) await api.delete(`/api/shares/${videoToken}`);
    if (imageToken) await api.delete(`/api/shares/${imageToken}`);
    if (libraryID) await api.delete(`/api/library/paths/${libraryID}`);
    await api.dispose();
  } catch {
    // 清理失败不影响测试结论
  }
  if (mediaDir) rmSync(mediaDir, { recursive: true, force: true });
});

test('无痕打开视频分享链接：不跳登录、视频确实在线播放、可下载原文件', async ({ page }) => {
  // 全新浏览器上下文默认无 cookie = 无痕/免登
  await page.goto(`/s/${videoToken}`);

  // 不被重定向到登录页
  await expect(page).toHaveURL(new RegExp(`/s/${videoToken}$`));
  await expect(page).not.toHaveURL(/\/login/);

  // 视频元素出现
  const video = page.locator('video');
  await expect(video).toBeVisible();

  // 真正播放：静音后 play()，断言 currentTime 推进（解码出帧）
  await video.evaluate((el: HTMLVideoElement) => { el.muted = true; return el.play(); });
  await expect
    .poll(async () => video.evaluate((el: HTMLVideoElement) => el.currentTime), { timeout: 10000 })
    .toBeGreaterThan(0);
  const readyState = await video.evaluate((el: HTMLVideoElement) => el.readyState);
  expect(readyState).toBeGreaterThanOrEqual(2); // ≥ HAVE_CURRENT_DATA

  // 下载原文件：点击触发下载事件，文件名为真实视频名
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('link', { name: /下载原文件/ }).click(),
  ]);
  expect(download.suggestedFilename()).toContain('.mp4');
});

test('无痕打开图片分享链接：图片真实渲染、可下载', async ({ page }) => {
  await page.goto(`/s/${imageToken}`);
  await expect(page).not.toHaveURL(/\/login/);

  const img = page.locator('img').first();
  await expect(img).toBeVisible();
  // 图片确实加载（解码出非零尺寸）
  await expect
    .poll(async () => img.evaluate((el: HTMLImageElement) => el.naturalWidth), { timeout: 10000 })
    .toBeGreaterThan(0);

  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('link', { name: /下载原文件/ }).click(),
  ]);
  expect(download.suggestedFilename()).toContain('.jpg');
});

test('无痕打开无效分享：显示过期提示且不跳登录', async ({ page }) => {
  await page.goto('/s/deadbeefdeadbeefdeadbeef');
  await expect(page).not.toHaveURL(/\/login/);
  await expect(page.getByText(/分享不存在或已过期/)).toBeVisible({ timeout: 10000 });
});
