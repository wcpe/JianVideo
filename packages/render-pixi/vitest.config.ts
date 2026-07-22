import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    coverage: {
      provider: "v8",
      reporter: ["text", "lcov"],
      // 会话层依赖 WebGL/DOM，单元覆盖落在纯逻辑模块；会话靠集成/E2E。
      exclude: [
        "**/node_modules/**",
        "**/dist/**",
        "**/scripts/**",
        "src/media-grid-session.ts",
        "**/*.test.ts",
      ],
      thresholds: {
        branches: 75,
        functions: 60,
        lines: 75,
        statements: 75,
      },
    },
  },
});
