import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.spec.ts',
  // 真服务 E2E 共享单一 SQLite、任务队列和运行期设置，必须串行执行。
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: 'html',
  use: {
    baseURL: process.env.TEST_BASE_URL || 'http://localhost:8080',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    // 每次清理并重建 E2E 专用数据根，再构建前端并启动真实服务，避免残留状态污染门禁。
    command:
      'node -e "const fs=require(\'fs\');fs.rmSync(\'.tmp/e2e-run\',{recursive:true,force:true});fs.mkdirSync(\'.tmp/e2e-run\',{recursive:true})" && npm --prefix frontend run build && go run .',
    url: 'http://localhost:8080/health',
    // E2E 始终拉起独立实例：用 .tmp 下的隔离库，避免污染开发库 jianvideo.db
    reuseExistingServer: false,
    timeout: 120000,
    env: {
      DB_PATH: '.tmp/e2e-run/e2e.db',
      SERVER_PORT: '8080',
    },
  },
});
