import { test, expect } from '@playwright/test';
import { mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BASE_URL, login } from './helpers';

test.use({ serviceWorkers: 'block' });

test('媒体库内容分型可创建、展示与更新', async ({ page }) => {
  const dir = mkdtempSync(join(tmpdir(), 'jianvideo-kind-e2e-'));
  const base = dir.split(/[\\/]/).pop()!;
  let createdId = 0;

  try {
    await login(page);
    await page.goto(`${BASE_URL}/library-manager`);
    await expect(page.getByRole('heading', { name: '存储库管理' })).toBeVisible();

    await page.getByPlaceholder(/输入目录路径/).fill(dir);
    await page.getByRole('textbox', { name: '新目录内容分型' }).click();
    await page.getByRole('option', { name: '分型：家庭录像' }).click();
    await page.getByRole('button', { name: '添加', exact: true }).click();

    await expect
      .poll(async () => {
        const res = await page.request.get('/api/library/paths');
        const data = await res.json();
        const created = (data.items ?? []).find((p: { id: number; path: string }) =>
          p.path.includes(base),
        );
        if (created) createdId = created.id;
        return created?.library_kind;
      })
      .toBe('home_video');

    const card = page.locator('.mantine-Card-root').filter({ hasText: base }).first();
    await expect(card.getByText('内容：家庭录像')).toBeVisible();

    await card.getByRole('textbox', { name: /内容分型/ }).click();
    await page.getByRole('option', { name: '分型：剧集' }).click();
    await expect(card.getByText('内容：剧集')).toBeVisible();

    await expect
      .poll(async () => {
        const res = await page.request.get('/api/library/paths');
        const data = await res.json();
        const created = (data.items ?? []).find((p: { id: number; path: string }) =>
          p.path.includes(base),
        );
        return created?.library_kind;
      })
      .toBe('series');

    mkdirSync('.tmp/screenshots', { recursive: true });
    await page.screenshot({ path: '.tmp/screenshots/library-kinds-real.png', fullPage: true });
  } finally {
    if (createdId) await page.request.delete(`/api/library/paths/${createdId}`);
    rmSync(dir, { recursive: true, force: true });
  }
});
