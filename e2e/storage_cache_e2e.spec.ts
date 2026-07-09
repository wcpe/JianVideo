import { test, expect } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { login } from './helpers';

test.use({ serviceWorkers: 'block' });

const screenshotDir = '.tmp/screenshots/fr2-048';

test.describe('存储与缓存管理端到端（FR2-048）', () => {
  test.beforeEach(() => {
    mkdirSync(screenshotDir, { recursive: true });
  });

  test('真实服务支持盘点、统计与 dry-run 清理', async ({ page }) => {
    const thumbPath = join('.tmp', 'thumbnails', 'fr2-048-e2e-thumb.jpg');
    mkdirSync(join('.tmp', 'thumbnails'), { recursive: true });
    writeFileSync(thumbPath, 'cache-thumb');

    await login(page);
    const inventory = await page.request.post('/api/storage/cache/inventory');
    expect(inventory.ok()).toBeTruthy();

    await page.goto('/storage-cache');
    await expect(page.getByRole('heading', { name: '缓存管理' })).toBeVisible();
    await expect(page.getByText('缩略图').first()).toBeVisible();
    await expect(page.getByText(/\d+ B/).first()).toBeVisible();

    await page.getByRole('button', { name: '预览清理' }).click();
    await expect(page.getByText(/预计影响/)).toBeVisible();
    await page.screenshot({ path: `${screenshotDir}/storage-cache-real.png`, fullPage: true });
  });
});
