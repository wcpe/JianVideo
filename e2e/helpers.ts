import { expect, type Page, type APIRequestContext } from '@playwright/test';

// E2E 公共前置：账户初始化与登录。
// FR-109 起取消 admin/admin 默认账户，全新 E2E 专用数据库首访为「初始化引导」（/setup）而非登录。
// 故所有需要登录的用例必须先经初始化引导建号，再走登录流程。

export const BASE_URL = process.env.TEST_BASE_URL || 'http://localhost:8080';
export const TEST_USER = 'admin';
export const TEST_PASS = 'admin';

// 确保系统已完成首次初始化（建好 admin 账户）。
// 幂等且并发安全：fullyParallel 下多用例可能同时 POST /api/auth/setup，
// 后端「检查无用户→建号」非原子，落败者会拿到 409 或 UNIQUE 约束 400。
// 故除 2xx 外，一律回查 setup-status：只要已不需初始化即视为就绪。
export async function ensureSetup(
  request: APIRequestContext,
  username = TEST_USER,
  password = TEST_PASS,
): Promise<void> {
  const res = await request.post('/api/auth/setup', {
    data: { username, password },
  });
  if (res.ok()) return;
  // 落败（409 已初始化 / 400 并发建号撞 UNIQUE）：回查状态确认账户确已就绪
  if (await isInitialized(request)) return;
  throw new Error(`初始化引导失败：HTTP ${res.status()} ${await res.text()}`);
}

// 回查初始化状态：needs_setup=false 表示已有账户、可直接登录。
async function isInitialized(request: APIRequestContext): Promise<boolean> {
  const status = await request.get('/api/auth/setup-status');
  if (!status.ok()) return false;
  return (await status.json()).needs_setup === false;
}

// 在登录页输入凭据并提交，成功后停留在首页（概览）。
// 调用前先确保已初始化，避免 /login 被守卫重定向到 /setup。
export async function login(
  page: Page,
  username = TEST_USER,
  password = TEST_PASS,
): Promise<void> {
  await ensureSetup(page.request, username, password);
  await page.goto('/login');
  await page.getByLabel('用户名').fill(username);
  await page.getByLabel('密码').fill(password);
  await page.getByRole('button', { name: '登录' }).click();
  // 登录成功后跳转首页概览，概览标题出现即视为进入受保护区
  await expect(page.getByRole('heading', { name: '概览' })).toBeVisible({ timeout: 10000 });
}
