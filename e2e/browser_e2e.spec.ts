import { test, expect } from '@playwright/test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BASE_URL, TEST_USER, TEST_PASS, ensureSetup, login } from './helpers';

// 浏览器端到端：覆盖当前前端的登录、路由守卫、主导航与登出。
// 选择器与路由期望对照 LoginPage / App.tsx 路由 / AppLayout / OverviewPage 编写。

// 禁用 Service Worker：应用为 PWA（autoUpdate），SW 首次安装接管页面会触发整页重载，
// 会在用例中途清空已填表单导致偶发失败；UI 流程测试不需要离线缓存，统一关闭以保证确定性。
test.use({ serviceWorkers: 'block' });

test.describe('JianVideo 浏览器端到端测试', () => {

  test.beforeEach(async ({ page }) => {
    // FR-109：先确保已完成初始化（建好 admin 账户），否则 /login 会被守卫重定向到 /setup
    await ensureSetup(page.request);
    // 进入应用并清空本地存储，确保每个用例从未登录态起步
    await page.goto(BASE_URL);
    await page.evaluate(() => localStorage.clear());
  });

  test('登录页面渲染', async ({ page }) => {
    await page.goto('/login');
    await expect(page.getByRole('heading', { name: 'JianVideo' })).toBeVisible();
    await expect(page.getByLabel('用户名')).toBeVisible();
    await expect(page.getByLabel('密码')).toBeVisible();
    await expect(page.getByRole('button', { name: '登录' })).toBeVisible();
  });

  test('成功登录并跳转到首页', async ({ page }) => {
    await login(page);
    await expect(page).toHaveURL(`${BASE_URL}/`);
    // 首页根路由（FR-117）为概览看板，时间轴已迁至 /timeline
    await expect(page.getByRole('heading', { name: '概览' })).toBeVisible();
  });

  test('错误密码登录失败', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('用户名').fill(TEST_USER);
    await page.getByLabel('密码').fill('wrongpassword');
    // 登录接口必须返回 401（凭据错误的真实结果）。
    // 注意：401 由 client.ts 响应拦截器整页跳回 /login，故不断言易朽的内联 Alert，
    // 而是断言接口 401 + 最终仍停留在登录页（未进入受保护区）。
    const [resp] = await Promise.all([
      page.waitForResponse(
        (r) => r.url().includes('/api/auth/login') && r.request().method() === 'POST',
      ),
      page.getByRole('button', { name: '登录' }).click(),
    ]);
    expect(resp.status()).toBe(401);
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole('button', { name: '登录' })).toBeVisible();
  });

  test('未认证访问受保护页面重定向', async ({ page }) => {
    await page.goto('/library-manager');
    // 未认证用户访问受保护路由应被重定向到登录页
    await expect(page).toHaveURL(/\/login/);
  });

  test('时间轴页渲染', async ({ page }) => {
    await login(page);
    // 时间轴迁至 /timeline（FR-117）：经导航进入后断言标题与搜索框
    await page.getByRole('link', { name: '时间轴' }).click();
    await expect(page).toHaveURL(/\/timeline/);
    await expect(page.getByRole('heading', { name: '时间轴' })).toBeVisible();
    await expect(page.getByPlaceholder(/搜索：文件名/)).toBeVisible();
  });

  test('管理页添加并移除媒体库目录', async ({ page }) => {
    // 后端要求本地路径真实存在，故创建临时目录作为样本；用例结束清理库记录与目录
    const dir = mkdtempSync(join(tmpdir(), 'jianvideo-browse-e2e-'));
    const base = dir.split(/[\\/]/).pop()!;
    let createdId = 0;
    try {
      await login(page);
      await page.getByRole('link', { name: '管理' }).click();
      await expect(page).toHaveURL(/\/library-manager/);

      await page.getByPlaceholder(/输入目录路径/).fill(dir);
      await page.getByRole('button', { name: '添加', exact: true }).click();

      // 经已登录上下文回查列表接口，确认新建库已落库并取回 ID（也用于 finally 清理）
      await expect
        .poll(async () => {
          const res = await page.request.get('/api/library/paths');
          const data = await res.json();
          const created = (data.items ?? []).find((p: { id: number; path: string }) => p.path.includes(base));
          if (created) createdId = created.id;
          return Boolean(created);
        }, { timeout: 10000 })
        .toBe(true);

      // 新目录也渲染到列表卡片中（卡片同一路径出现在多处文本节点，取首个即可）
      await expect(page.getByText(new RegExp(base)).first()).toBeVisible();
    } finally {
      if (createdId) await page.request.delete(`/api/library/paths/${createdId}`);
      rmSync(dir, { recursive: true, force: true });
    }
  });

  test('导航在目录与概览间切换', async ({ page }) => {
    await login(page);
    // 先离开首页进入目录，再点击导航回到概览首页，验证路由切换生效
    await page.getByRole('link', { name: '目录' }).click();
    await expect(page).toHaveURL(/\/browse/);

    await page.getByRole('link', { name: '概览' }).click();
    await expect(page).toHaveURL(`${BASE_URL}/`);
    await expect(page.getByRole('heading', { name: '概览' })).toBeVisible();
  });

  test('导航到目录浏览页', async ({ page }) => {
    await login(page);
    await page.getByRole('link', { name: '目录' }).click();
    await expect(page).toHaveURL(/\/browse/);
    await expect(page.getByRole('heading', { name: '目录浏览' })).toBeVisible();
  });

  test('搜索功能', async ({ page }) => {
    await login(page);
    // 搜索框在时间轴页（FR-117 后迁至 /timeline）
    await page.getByRole('link', { name: '时间轴' }).click();
    await expect(page).toHaveURL(/\/timeline/);
    const searchInput = page.getByPlaceholder(/搜索：文件名/);
    await expect(searchInput).toBeVisible();
    await searchInput.fill('test');
    await expect(searchInput).toHaveValue('test');
  });

  test('登出功能', async ({ page }) => {
    await login(page);
    // 登出收进头像下拉菜单（FR-95）：先点用户菜单，再点「退出登录」项
    await page.getByRole('button', { name: `用户菜单：${TEST_USER}` }).click();
    await page.getByRole('menuitem', { name: '退出登录' }).click();
    await expect(page).toHaveURL(/\/login/);
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
