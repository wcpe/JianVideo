import { test, expect, type APIRequestContext } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { login } from './helpers';

test.use({ serviceWorkers: 'block' });

const screenshotDir = '.tmp/screenshots/fr2-061';
const hasFfmpeg = (() => {
  try {
    execFileSync('ffmpeg', ['-version'], { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
})();

test('精确内容哈希去重可回填、展示并保留相似重复入口', async ({ page }) => {
  test.setTimeout(90000);
  test.skip(!hasFfmpeg, '未检测到 ffmpeg，跳过真实媒体精确去重 E2E');
  const mediaDir = mkdtempSync(join(tmpdir(), 'jianvideo-fr2-061-'));
  let libraryID = 0;

  try {
    mkdirSync(screenshotDir, { recursive: true });
    writeVideoFixture(join(mediaDir, 'exact-a.mp4'), 1);
    copyFileSync(join(mediaDir, 'exact-a.mp4'), join(mediaDir, 'exact-b.mp4'));
    writeVideoFixture(join(mediaDir, 'unique.mp4'), 2);

    await login(page);
    const createLib = await page.request.post('/api/library/paths', {
      data: {
        path: mediaDir.replace(/\\/g, '/'),
        type: 'local',
        label: 'FR2-061 精确重复库',
      },
    });
    expect(createLib.ok()).toBeTruthy();
    libraryID = (await createLib.json()).id;

    const scan = await page.request.post(`/api/library/scan/${libraryID}`, {
      params: { mode: 'full' },
    });
    expect(scan.ok()).toBeTruthy();
    await waitScanDone(page.request, (await scan.json()).task_id);
    await expect.poll(() => mediaTotal(page.request, libraryID), { timeout: 15000 }).toBe(3);

    const backfill = await page.request.post('/api/library/file-hashes/backfill');
    expect(backfill.status()).toBe(202);
    await expect.poll(() => exactDuplicateSize(page.request), { timeout: 20000 }).toBe(2);

    await page.goto('/duplicates');
    await expect(page.getByRole('tab', { name: '精确重复' })).toBeVisible();
    await expect(page.getByText('exact-a.mp4')).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('exact-b.mp4')).toBeVisible();
    await expect(page.getByText('unique.mp4')).toHaveCount(0);

    await page.getByRole('tab', { name: '相似重复' }).click();
    await expect(page.getByRole('button', { name: /扫描相似重复/ })).toBeVisible();
    await page.screenshot({ path: `${screenshotDir}/file-hash-dedup-real.png`, fullPage: true });
  } finally {
    if (libraryID && !page.isClosed()) {
      await page.request.delete(`/api/library/paths/${libraryID}`);
    }
    await removeTempDir(mediaDir);
  }
});

async function mediaTotal(request: APIRequestContext, libraryID: number) {
  const res = await request.get(`/api/library/media?library_id=${libraryID}&page_size=100`);
  expect(res.ok()).toBeTruthy();
  return (await res.json()).total;
}

async function exactDuplicateSize(request: APIRequestContext) {
  const res = await request.get('/api/library/duplicates/exact');
  expect(res.ok()).toBeTruthy();
  const groups = ((await res.json()).groups ?? []) as Array<{ items: unknown[] }>;
  return groups[0]?.items.length ?? 0;
}

async function waitScanDone(request: APIRequestContext, taskID: number) {
  await expect
    .poll(
      async () => {
        const res = await request.get('/api/library/scan/tasks');
        expect(res.ok()).toBeTruthy();
        const tasks = (await res.json()).tasks ?? [];
        const task = tasks.find((item: { id: number }) => item.id === taskID);
        if (!task) return 'missing';
        if (task.status === 'error') throw new Error(task.error || '扫描任务失败');
        return task.status;
      },
      { timeout: 15000 },
    )
    .toBe('completed');
}

function writeVideoFixture(path: string, duration: number) {
  execFileSync(
    'ffmpeg',
    [
      '-y',
      '-f',
      'lavfi',
      '-i',
      `testsrc=duration=${duration}:size=160x120:rate=10`,
      '-c:v',
      'libx264',
      '-pix_fmt',
      'yuv420p',
      path,
    ],
    { stdio: 'ignore' },
  );
}

async function removeTempDir(path: string) {
  for (let attempt = 0; attempt < 20; attempt++) {
    if (!existsSync(path)) return;
    try {
      rmSync(path, { recursive: true, force: true });
      return;
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }
  if (existsSync(path)) {
    rmSync(path, { recursive: true, force: true });
  }
}
