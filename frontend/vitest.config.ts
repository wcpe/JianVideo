import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'node',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    css: false,
    // 覆盖率插桩会放慢 Mantine 弹层与异步 UI 测试，统一抬高单测超时避免门禁误红。
    testTimeout: 15_000,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'lcov'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/**/*.spec.{ts,tsx}', 'src/main.tsx'],
      // 覆盖率门禁阈值（FR-126，见 ADR-0047）：按实测务实定档（实测 stmts/lines≈78、branches≈81、funcs≈63）
      thresholds: {
        lines: 75,
        statements: 75,
        functions: 60,
        branches: 75,
      },
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
});
