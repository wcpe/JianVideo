import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    coverage: {
      provider: "v8",
      reporter: ["text", "lcov"],
      thresholds: {
        branches: 75,
        functions: 60,
        lines: 75,
        statements: 75,
      },
    },
  },
});
