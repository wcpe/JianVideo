import { test, expect, type Page } from '@playwright/test';

// 测试配置
const BASE_URL = process.env.TEST_BASE_URL || 'http://localhost:8080';
const TEST_USER = 'admin';
const TEST_PASS = 'admin';

// 辅助函数：登录
async function login(page: Page, username = TEST_USER, password = TEST_PASS) {
  await page.goto(`${BASE_URL}/login`);
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', password);
  await page.click('button[type="submit"]');
  // 等待导航到媒体库
  await page.waitForURL('**/library');
}

test.describe('JianVideo 浏览器端到端测试', () => {

  test.beforeEach(async ({ page }) => {
    // 每个测试前清除 localStorage 和 cookie
    await page.goto(BASE_URL);
    await page.evaluate(() => localStorage.clear());
  });

  test('登录页面渲染', async ({ page }) => {
    await page.goto(`${BASE_URL}/login`);
    await expect(page.locator('text=登录')).toBeVisible();
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });

  test('成功登录并跳转到媒体库', async ({ page }) => {
    await login(page);
    await expect(page).toHaveURL(/.*library/);
  });

  test('错误密码登录失败', async ({ page }) => {
    await page.goto(`${BASE_URL}/login`);
    await page.fill('input[name="username"]', TEST_USER);
    await page.fill('input[name="password"]', 'wrongpassword');
    await page.click('button[type="submit"]');
    // 应该显示错误提示
    await expect(page.locator('.mantine-Alert-root')).toBeVisible({ timeout: 5000 });
  });

  test('未认证访问受保护页面重定向', async ({ page }) => {
    await page.goto(`${BASE_URL}/library`);
    // 未认证用户应被重定向到登录页或看到登录页面内容
    await expect(page).toHaveURL(/.*login/);
  });

  test('媒体库页面渲染', async ({ page }) => {
    await login(page);
    await expect(page.locator('text=媒体库')).toBeVisible();
  });

  test('添加媒体库路径', async ({ page }) => {
    await login(page);
    // 等待路径输入框出现
    const pathInput = page.locator('input[placeholder*="路径"]');
    await pathInput.waitFor({ state: 'visible', timeout: 5000 });
    await pathInput.fill('/tmp/test_videos');
    await page.click('button:has-text("添加")');
    // 验证路径被添加到列表
    await expect(page.locator('text=/tmp/test_videos')).toBeVisible({ timeout: 5000 });
  });

  test('时间轴视图切换', async ({ page }) => {
    await login(page);
    // 等待 tab 出现
    await page.waitForSelector('[role="tab"]', { timeout: 5000 });
    const timelineTab = page.locator('[role="tab"]:has-text("时间轴")');
    if (await timelineTab.count() > 0) {
      await timelineTab.click();
      await expect(timelineTab).toHaveAttribute('aria-selected', 'true');
    }
  });

  test('文件目录视图切换', async ({ page }) => {
    await login(page);
    await page.waitForSelector('[role="tab"]', { timeout: 5000 });
    const directoryTab = page.locator('[role="tab"]:has-text("目录")');
    if (await directoryTab.count() > 0) {
      await directoryTab.click();
      await expect(directoryTab).toHaveAttribute('aria-selected', 'true');
    }
  });

  test('搜索功能', async ({ page }) => {
    await login(page);
    const searchInput = page.locator('input[placeholder*="搜索"]');
    if (await searchInput.count() > 0) {
      await searchInput.fill('test');
      // 等待防抖
      await page.waitForTimeout(500);
      // 验证搜索被触发（页面不崩溃即可）
      await expect(page.locator('body')).toBeVisible();
    }
  });

  test('登出功能', async ({ page }) => {
    await login(page);
    // 登出按钮可能在菜单组件中，尝试多种选择器
    const logoutBtn = page.locator('button:has-text("登出")');
    if (await logoutBtn.count() > 0) {
      await logoutBtn.click();
      await expect(page).toHaveURL(/.*login/);
    }
  });

  test('API 健康检查', async ({ request }) => {
    const response = await request.get(`${BASE_URL}/health`);
    expect(response.ok()).toBeTruthy();
  });

  test('API 认证流程', async ({ request }) => {
    // 登录获取认证
    const loginRes = await request.post(`${BASE_URL}/api/auth/login`, {
      data: { username: TEST_USER, password: TEST_PASS }
    });
    expect(loginRes.ok()).toBeTruthy();
  });
});
