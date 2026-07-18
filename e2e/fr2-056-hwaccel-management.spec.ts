import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { ensureSetup, login } from './helpers';

const screenshotDir = process.env.FR2056_SCREENSHOT_DIR || join('.tmp', 'screenshots', 'fr2-056');

test.use({ serviceWorkers: 'block' });
test.describe.configure({ mode: 'serial' });

test.describe('FR2-056 硬件转码加速管理面板', () => {
  test.beforeEach(async ({ page }) => {
    mkdirSync(screenshotDir, { recursive: true });
    await ensureSetup(page.request);
    await login(page);
  });

  test.afterEach(async ({ page }) => {
    await page.evaluate(async () => {
      await fetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          settings: {
            transcode_hwaccel_mode: 'auto',
            transcode_hwaccel_fallback: '1',
          },
        }),
      });
    });
  });

  test('保存硬件策略并强制重测能力', async ({ page }) => {
    test.setTimeout(180000);

    await page.goto('/system?tab=hwaccel');
    await expect(page.getByRole('heading', { name: '硬件加速' }).first()).toBeVisible();
    const savePolicyButton = page.getByRole('button', { name: '保存策略' });
    await expect(savePolicyButton).toBeVisible({ timeout: 30_000 });
    await expect(page.getByText('硬件策略')).toBeVisible();

    await page.getByText('QSV', { exact: true }).click();
    const fallback = page.getByRole('switch', { name: '硬件不可用时软件回退' });
    if (await fallback.isChecked()) {
      await page.locator('label').filter({ hasText: '软件回退' }).click();
    }
    await savePolicyButton.click();

    await expect
      .poll(async () => {
        const body = await page.evaluate(async () => {
          const res = await fetch('/api/settings');
          return (await res.json()) as { settings: Record<string, string> };
        });
        return {
          mode: body.settings.transcode_hwaccel_mode,
          fallback: body.settings.transcode_hwaccel_fallback,
        };
      })
      .toEqual({ mode: 'qsv', fallback: '0' });
    await expect
      .poll(async () => {
        const body = await page.evaluate(async () => {
          const res = await fetch('/api/audit/events?scope=system&action=settings.updated&limit=20');
          return (await res.json()) as {
            items: Array<{ action: string; resource_type: string; after_json: Record<string, string> | null }>;
          };
        });
        return body.items.some(
          (item) =>
            item.action === 'settings.updated' &&
            item.resource_type === 'settings' &&
            item.after_json?.transcode_hwaccel_mode === 'qsv' &&
            item.after_json?.transcode_hwaccel_fallback === '0',
        );
      })
      .toBe(true);

    await page.reload();
    await expect(page.getByRole('heading', { name: '硬件加速' }).first()).toBeVisible();
    await expect(page.locator('input[value="qsv"]')).toBeChecked({ timeout: 30_000 });
    await expect(page.getByRole('switch', { name: '硬件不可用时软件回退' })).not.toBeChecked();
    await page.screenshot({
      path: join(screenshotDir, 'hwaccel-policy-saved.png'),
      fullPage: true,
    });

    const responsePromise = page.waitForResponse(
      (response) =>
        response.url().includes('/api/system/codec-test') &&
        response.url().includes('force=true') &&
        response.status() === 200,
      { timeout: 180000 },
    );
    await page.getByRole('button', { name: '强制重测' }).click();
    const response = await responsePromise;
    const retest = (await response.json()) as { ffmpeg_available: boolean; from_cache: boolean };
    expect(retest.from_cache).toBe(false);

    await expect
      .poll(async () => {
        const body = await page.evaluate(async () => {
          const res = await fetch('/api/audit/events?scope=system&limit=20');
          return (await res.json()) as { items: Array<{ action: string }> };
        });
        return body.items.map((item) => item.action);
      })
      .toContain('codec_probe.retested');
    await page.screenshot({
      path: join(screenshotDir, 'hwaccel-force-retest.png'),
      fullPage: true,
    });
  });
});
