import { chromium, expect } from '@playwright/test';
import fs from 'node:fs/promises';
import path from 'node:path';

const baseUrl = process.env.WIKI_BASE_URL ?? 'http://127.0.0.1:4175';
const screenshotDir = path.resolve(process.env.WIKI_SCREENSHOT_DIR ?? '.tmp/e2e/fr2-005');
const desktopScreenshot = path.join(screenshotDir, 'wiki-desktop.png');
const mobileScreenshot = path.join(screenshotDir, 'wiki-mobile.png');

async function assertMuseum(page) {
  await page.goto(baseUrl, { waitUntil: 'networkidle' });
  await expect(page.getByRole('heading', { name: 'JianVideo Wiki' })).toBeVisible();

  await page.getByTestId('wiki-search').fill('HLS');
  await expect(page.getByTestId('wiki-preview-hls-preview-card')).toContainText('HLS 预览卡');
  await expect(page.getByTestId('wiki-snippet-hls-preview-card')).toContainText("from '@jianvideo/ui'");

  await page.getByTestId('wiki-search').fill('');
  await page.getByTestId('wiki-group-task').click();
  await expect(page.getByTestId('wiki-preview-task-status')).toContainText('转码任务状态');
  await expect(page.getByTestId('wiki-preview-ai-task-status')).toContainText('AI 任务状态');

  await page.getByTestId('wiki-scenario-select').selectOption('hls-pending');
  await expect(page.getByTestId('wiki-selected-scenario')).toContainText('HLS 生成中');
  await page.getByTestId('wiki-scenario-select').selectOption('transcode-failed');
  await expect(page.getByTestId('wiki-selected-scenario')).toContainText('转码失败');
  await page.getByTestId('wiki-scenario-select').selectOption('ai-review-pending');
  await expect(page.getByTestId('wiki-selected-scenario')).toContainText('AI 审核待处理');

  await page.getByTestId('wiki-group-space').click();
  await expect(page.getByTestId('wiki-preview-space-permission-status')).toContainText('Space 权限状态');
  await page.getByTestId('wiki-scenario-select').selectOption('permission-denied');
  await expect(page.getByTestId('wiki-selected-scenario')).toContainText('Space 权限不足');

  await page.getByTestId('wiki-state-error').click();
  await expect(page.getByTestId('wiki-active-state')).toContainText('error');
}

async function main() {
  await fs.mkdir(screenshotDir, { recursive: true });
  const browser = await chromium.launch();
  try {
    const desktop = await browser.newPage({ viewport: { width: 1440, height: 960 } });
    await assertMuseum(desktop);
    await desktop.screenshot({ fullPage: true, path: desktopScreenshot });
    await desktop.close();

    const mobile = await browser.newPage({
      isMobile: true,
      userAgent:
        'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1',
      viewport: { width: 390, height: 844 },
    });
    await assertMuseum(mobile);
    await mobile.screenshot({ fullPage: true, path: mobileScreenshot });
    await mobile.close();

    console.log(`desktop: ${desktopScreenshot}`);
    console.log(`mobile: ${mobileScreenshot}`);
  } finally {
    await browser.close();
  }
}

main().catch((error) => {
  console.error('FR2-005 wiki E2E 失败');
  console.error(error);
  process.exit(1);
});
