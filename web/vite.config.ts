import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The dev server proxies /api to the Go backend; in production one origin serves both.
const backend = process.env.METERWISE_API_URL ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/api": { target: backend, changeOrigin: true } } },
  build: { outDir: "dist", sourcemap: true },
  test: {
    environment: "jsdom",
    setupFiles: ["src/test/setup.ts"],
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts", "src/**/*.tsx"],
      exclude: [
        "src/**/*.test.ts",
        "src/**/*.test.tsx",
        "src/main.tsx",
        "src/test/**",
        "src/contract/generated.ts",
      ],
      // Floor 70 % (docs/context-budget.md). Raise, never lower.
      thresholds: { statements: 70, branches: 70, functions: 70, lines: 70 },
    },
  },
});
