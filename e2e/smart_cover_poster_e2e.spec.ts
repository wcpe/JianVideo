// 覆盖 PRD 智能封面/海报
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { login } from './helpers';

test.use({ serviceWorkers: 'block' });
test.describe.configure({ mode: 'serial' });

const hasFfmpeg = (() => {
  try {
    execFileSync('ffmpeg', ['-version'], { stdio: 'ignore' });
    execFileSync('ffprobe', ['-version'], { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
})();

test('详情页选择封面后同步列表，清理缓存仍保留语义并可重建', async ({ page }) => {
  test.setTimeout(120000);
  test.skip(!hasFfmpeg, '需要真实 ffmpeg/ffprobe 素材完成 E2E');
  const mediaDir = mkdtempSync(join(tmpdir(), 'jianvideo-cover-poster-'));
  const videoName = 'cover-poster-smart-cover.mp4';
  let libraryID = 0;

  try {
    writeVideoFixture(join(mediaDir, videoName));
    await login(page);
    libraryID = await createLibrary(page.request, mediaDir);
    const mediaID = await scanVideo(page.request, libraryID, videoName);

    await page.goto('/timeline');
    const listThumbnail = page.getByAltText(videoName).first();
    await expect(listThumbnail).toBeVisible({ timeout: 15000 });
    await listThumbnail.click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await dialog.getByRole('button', { name: '生成封面候选' }).click();
    await expect(dialog.getByRole('button', { name: /^选择 .* 秒封面$/ })).toHaveCount(5, {
      timeout: 30000,
    });

    const generated = await loadCovers(page.request, mediaID);
    expect(generated.candidates).toHaveLength(5);
    const chosen = generated.candidates.at(-1)!;
    expect(chosen.fingerprint).not.toBe(generated.cover?.selected_fingerprint);
    await dialog
      .getByRole('button', { name: `选择 ${chosen.timestamp_seconds.toFixed(1)} 秒封面` })
      .click();
    await expect(dialog.getByText('人工选择')).toBeVisible();

    const selected = await loadCovers(page.request, mediaID);
    expect(selected.cover).toMatchObject({
      manual: true,
      selected_fingerprint: chosen.fingerprint,
      selected_asset_id: chosen.asset_id,
    });
    const oldAssetID = selected.cover!.selected_asset_id;
    await expect(listThumbnail).toHaveAttribute('src', /cover_v=\d+/);
    await expectSelectedThumbnail(page.request, mediaID, chosen.id);

    const cleanTaskID = await cleanCoverCache(page.request);
    await waitTask(page.request, cleanTaskID);
    const stale = await loadCovers(page.request, mediaID);
    expect(stale.cover).toMatchObject({
      manual: true,
      selected_fingerprint: chosen.fingerprint,
      selected_asset_id: oldAssetID,
    });

    await dialog.getByRole('button', { name: '重新生成封面候选' }).click();
    await expect
      .poll(async () => (await loadCovers(page.request, mediaID)).cover?.selected_asset_id, {
        timeout: 30000,
      })
      .not.toBe(oldAssetID);
    const restored = await loadCovers(page.request, mediaID);
    expect(restored.cover).toMatchObject({
      manual: true,
      selected_fingerprint: chosen.fingerprint,
    });
    expect(restored.cover!.selected_asset_id).toBeGreaterThan(0);
    await expectSelectedThumbnail(page.request, mediaID, chosen.id);
  } finally {
    if (libraryID && !page.isClosed()) {
      await page.request.delete(`/api/library/paths/${libraryID}`);
    }
    if (existsSync(mediaDir)) rmSync(mediaDir, { recursive: true, force: true });
  }
});

interface CoverCandidate {
  id: number;
  asset_id: number;
  timestamp_seconds: number;
  fingerprint: string;
  image_url: string;
}

interface CoverResponse {
  cover: {
    selected_asset_id: number;
    selected_fingerprint: string;
    manual: boolean;
  } | null;
  candidates: CoverCandidate[];
}

function writeVideoFixture(path: string): void {
  execFileSync(
    'ffmpeg',
    [
      '-y',
      '-f',
      'lavfi',
      '-i',
      'testsrc2=duration=6:size=640x360:rate=15',
      '-c:v',
      'mpeg4',
      '-q:v',
      '4',
      '-movflags',
      '+faststart',
      path,
    ],
    { stdio: 'ignore' },
  );
}

async function createLibrary(request: APIRequestContext, mediaDir: string): Promise<number> {
  const response = await request.post('/api/library/paths', {
    data: {
      path: mediaDir.replace(/\\/g, '/'),
      type: 'local',
      label: '智能封面专项 E2E',
    },
  });
  if (!response.ok()) {
    throw new Error(`创建 媒体库失败: HTTP ${response.status()} ${await response.text()}`);
  }
  return ((await response.json()) as { id: number }).id;
}

async function scanVideo(
  request: APIRequestContext,
  libraryID: number,
  videoName: string,
): Promise<number> {
  const scan = await request.post(`/api/library/scan/${libraryID}`, { params: { mode: 'full' } });
  expect(scan.ok()).toBeTruthy();
  await waitScan(request, ((await scan.json()) as { task_id: number }).task_id);
  const response = await request.get('/api/library/media', {
    params: { library_id: libraryID, page_size: 100 },
  });
  expect(response.ok()).toBeTruthy();
  const items = ((await response.json()) as { items: Array<{ id: number; file_name: string }> }).items;
  const video = items.find((item) => item.file_name === videoName);
  expect(video).toBeTruthy();
  return video!.id;
}

async function waitScan(request: APIRequestContext, taskID: number): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await request.get('/api/library/scan/tasks');
        expect(response.ok()).toBeTruthy();
        const tasks = (
          (await response.json()) as {
            tasks: Array<{ id: number; status: string; error?: string }>;
          }
        ).tasks;
        const task = tasks.find((item) => item.id === taskID);
        if (task?.status === 'error') throw new Error(task.error || '扫描任务失败');
        return task?.status ?? 'missing';
      },
      { timeout: 20000 },
    )
    .toBe('completed');
}

async function loadCovers(request: APIRequestContext, mediaID: number): Promise<CoverResponse> {
  const response = await request.get(`/api/library/media/${mediaID}/covers`);
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as CoverResponse;
}

async function cleanCoverCache(request: APIRequestContext): Promise<number> {
  const response = await request.post('/api/storage/cache/clean', {
    data: { dry_run: false, kinds: ['cover'] },
  });
  expect(response.status()).toBe(202);
  return ((await response.json()) as { task_id: number }).task_id;
}

async function waitTask(request: APIRequestContext, taskID: number): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await request.get(`/api/tasks/${taskID}`);
        expect(response.ok()).toBeTruthy();
        const task = (await response.json()) as { status: string; error?: string };
        if (task.status === 'failed' || task.status === 'canceled') {
          throw new Error(task.error || `任务 ${taskID} 未成功`);
        }
        return task.status;
      },
      { timeout: 30000 },
    )
    .toBe('succeeded');
}

async function expectSelectedThumbnail(
  request: APIRequestContext,
  mediaID: number,
  candidateID: number,
): Promise<void> {
  const [thumbnail, candidate] = await Promise.all([
    request.get(`/api/library/thumbnail/${mediaID}?size=320&cover_v=${Date.now()}`),
    request.get(`/api/library/media/${mediaID}/covers/${candidateID}/image?cover_v=${Date.now()}`),
  ]);
  expect(thumbnail.status()).toBe(200);
  expect(candidate.status()).toBe(200);
  expect(Buffer.compare(await thumbnail.body(), await candidate.body())).toBe(0);
}
