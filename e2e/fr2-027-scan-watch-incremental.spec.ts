import { test, expect, type APIRequestContext } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BASE_URL, login } from './helpers';

test.use({ serviceWorkers: 'block' });

const screenshotDir = '.tmp/screenshots/fr2-027';

test('全量扫描将丢失文件标记为 missing 并从常规列表隐藏', async ({ page }) => {
  const mediaDir = await mkdtemp(join(tmpdir(), 'jianvideo-fr2-027-'));
  const mediaPath = join(mediaDir, 'fr2-027-scan.mp4');
  let libraryID = 0;

  try {
    mkdirSync(screenshotDir, { recursive: true });
    writeMediaFixture(mediaPath);

    await login(page);
    const createLib = await page.request.post('/api/library/paths', {
      data: { path: mediaDir.replace(/\\/g, '/'), type: 'local', label: '扫描变更 E2E' },
    });
    expect(createLib.ok()).toBeTruthy();
    libraryID = (await createLib.json()).id;

    const firstScan = await page.request.post(`/api/library/scan/${libraryID}`, {
      params: { mode: 'full' },
    });
    expect(firstScan.ok()).toBeTruthy();
    await waitScanDone(page.request, (await firstScan.json()).task_id);

    await expect
      .poll(async () => mediaTotal(page.request, libraryID), { timeout: 10000 })
      .toBe(1);

    await removePathWithRetry(mediaPath, false);
    const secondScan = await page.request.post(`/api/library/scan/${libraryID}`, {
      params: { mode: 'full' },
    });
    expect(secondScan.ok()).toBeTruthy();
    await waitScanDone(page.request, (await secondScan.json()).task_id);

    await expect
      .poll(async () => mediaTotal(page.request, libraryID), { timeout: 10000 })
      .toBe(0);
    const recycle = await page.request.get('/api/library/recycle');
    expect(recycle.ok()).toBeTruthy();
    expect((await recycle.json()).items ?? []).toHaveLength(0);

    await page.goto(`${BASE_URL}/library-manager`);
    await expect(page.getByRole('heading', { name: '存储库管理' })).toBeVisible();
    await page.screenshot({ path: `${screenshotDir}/scan-missing-hidden.png`, fullPage: true });

    await page.goto(`${BASE_URL}/tasks`);
    await expect(page.getByText('library.scan').first()).toBeVisible({ timeout: 10000 });
    await page.screenshot({ path: `${screenshotDir}/scan-task-queue.png`, fullPage: true });
  } finally {
    if (libraryID) await page.request.delete(`/api/library/paths/${libraryID}`);
    await removePathWithRetry(mediaDir, true);
  }
});

async function removePathWithRetry(path: string, recursive: boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    if (!existsSync(path)) return;
    try {
      rmSync(path, { recursive, force: true });
      return;
    } catch (error) {
      if (attempt === 19) throw error;
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }
}

async function mediaTotal(request: APIRequestContext, libraryID: number) {
  const res = await request.get(`/api/library/media?library_id=${libraryID}&page_size=100`);
  expect(res.ok()).toBeTruthy();
  return (await res.json()).total;
}

function writeMediaFixture(path: string) {
  try {
    execFileSync(
      'ffmpeg',
      [
        '-y',
        '-f',
        'lavfi',
        '-i',
        'testsrc=duration=1:size=160x120:rate=10',
        '-c:v',
        'libx264',
        '-pix_fmt',
        'yuv420p',
        path,
      ],
      { stdio: 'ignore' },
    );
  } catch {
    writeFileSync(path, Buffer.from('fr2-027'));
  }
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
