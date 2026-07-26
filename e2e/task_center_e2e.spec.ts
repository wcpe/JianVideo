import { test, expect } from '@playwright/test';
import { mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ensureSetup, login } from './helpers';

test.use({ serviceWorkers: 'block' });

const screenshotDir = '.tmp/screenshots';

test.describe('任务中心端到端', () => {
  test.beforeEach(async ({ page }) => {
    await ensureSetup(page.request);
    mkdirSync(screenshotDir, { recursive: true });
  });

  test('任务中心 UI 支持筛选、取消与重试', async ({ page }) => {
    await page.route('**/api/tasks/stats**', async (route) => {
      await route.fulfill({
        json: {
          total: 2,
          by_status: { pending: 0, running: 1, succeeded: 0, failed: 1, canceled: 0 },
          by_type: { 'library.scan': 1, 'transcode.hls': 1 },
        },
      });
    });
    await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
      const url = new URL(route.request().url());
      const failedOnly = url.searchParams.get('status') === 'failed';
      const items = failedOnly
        ? [
            {
              id: '12',
              scope: 'space',
              space_id: 'space-default',
              type: 'transcode.hls',
              status: 'failed',
              priority: 5,
              attempts: 1,
              max_attempts: 3,
              progress: 0.4,
              resource_type: 'media',
              resource_id: '99',
              error: '编码器不可用',
              created_at: '2026-07-08T08:00:00Z',
              updated_at: '2026-07-08T08:01:00Z',
            },
          ]
        : [
            {
              id: '11',
              scope: 'space',
              space_id: 'space-default',
              type: 'library.scan',
              status: 'running',
              priority: 10,
              attempts: 0,
              max_attempts: 1,
              progress: 0.35,
              resource_type: 'library',
              resource_id: '1',
              error: null,
              created_at: '2026-07-08T08:00:00Z',
              updated_at: '2026-07-08T08:01:00Z',
            },
          ];
      await route.fulfill({ json: { items, page: 1, page_size: 20, total: items.length } });
    });
    await page.route('**/api/tasks/11/cancel', async (route) => {
      await route.fulfill({
        json: {
          id: '11',
          scope: 'space',
          space_id: 'space-default',
          type: 'library.scan',
          status: 'canceled',
          priority: 10,
          attempts: 0,
          max_attempts: 1,
          progress: 0.35,
          resource_type: 'library',
          resource_id: '1',
          error: null,
          created_at: '2026-07-08T08:00:00Z',
          updated_at: '2026-07-08T08:02:00Z',
        },
      });
    });
    await page.route('**/api/tasks/12/retry', async (route) => {
      await route.fulfill({
        json: {
          id: '12',
          scope: 'space',
          space_id: 'space-default',
          type: 'transcode.hls',
          status: 'pending',
          priority: 5,
          attempts: 0,
          max_attempts: 3,
          progress: 0,
          resource_type: 'media',
          resource_id: '99',
          error: null,
          created_at: '2026-07-08T08:00:00Z',
          updated_at: '2026-07-08T08:02:00Z',
        },
      });
    });

    await login(page);
    await page.getByRole('link', { name: '任务' }).click();
    await expect(page).toHaveURL(/\/tasks/);
    await expect(page.getByRole('heading', { name: '任务中心' })).toBeVisible();
    await expect(page.getByText('library.scan')).toBeVisible();

    await page.getByLabel('状态').first().click();
    await page.getByRole('option', { name: '失败' }).click();
    await page.getByRole('button', { name: '查询' }).click();
    await expect(page.getByText('transcode.hls')).toBeVisible();
    await expect(page.getByText('编码器不可用')).toBeVisible();
    await page.screenshot({ path: `${screenshotDir}/task-center-mock.png`, fullPage: true });
  });

  test('真实服务中扫描任务进入任务中心', async ({ page }) => {
    const dir = mkdtempSync(join(tmpdir(), 'jianvideo-task-e2e-'));
    let libraryID = 0;
    try {
      await login(page);
      const createLib = await page.request.post('/api/library/paths', {
        data: { path: dir.replace(/\\/g, '/'), type: 'local', label: '任务中心 E2E' },
      });
      expect(createLib.ok()).toBeTruthy();
      libraryID = (await createLib.json()).id;

      const scan = await page.request.post(`/api/library/scan/${libraryID}`);
      expect(scan.ok()).toBeTruthy();
      await expect
        .poll(async () => {
          const res = await page.request.get('/api/tasks?page_size=20');
          const body = await res.json();
          return (body.items ?? []).some((item: { type: string }) => item.type === 'library.scan');
        }, { timeout: 15000 })
        .toBe(true);

      await page.goto('/tasks');
      await expect(page.getByRole('heading', { name: '任务中心' })).toBeVisible();
      await expect(page.getByText('library.scan').first()).toBeVisible({ timeout: 10000 });
      await page.screenshot({ path: `${screenshotDir}/task-center-real.png`, fullPage: true });
    } finally {
      if (libraryID) await page.request.delete(`/api/library/paths/${libraryID}`);
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
