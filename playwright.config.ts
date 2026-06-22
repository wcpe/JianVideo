import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.spec.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
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
    // 先构建前端（go:embed frontend/dist 需最新产物），再起服务，确保测的是当次源码
    command: 'npm --prefix frontend run build && go run .',
    url: 'http://localhost:8080/health',
    // E2E 始终拉起独立实例：用 .tmp 下的隔离库，避免污染开发库 jianvideo.db
    reuseExistingServer: false,
    timeout: 120000,
    env: {
      DB_PATH: '.tmp/e2e.db',
      SERVER_PORT: '8080',
    },
  },
});
