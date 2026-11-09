import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The dev server proxies /api to the Go backend; in production one origin serves both.
const backend = process.env.METERWISE_API_URL ?? "http://127.0.0.1:8081";

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/api": { target: backend, changeOrigin: true } } },
  build: {
    outDir: "dist",
    // Hidden: maps are emitted for error reporting but not linked from the bundles.
    sourcemap: "hidden",
    rollupOptions: {
      output: {
        manualChunks(id: string) {
          if (!id.includes("node_modules")) return undefined;
          if (
            /node_modules\/(react|react-dom|react-router|react-router-dom|scheduler)\//.test(id)
          ) {
            return "vendor-react";
          }
          if (/node_modules\/(@tanstack|zustand)\//.test(id)) return "vendor-query";
          if (/node_modules\/(antd|@ant-design|@rc-component|rc-[^/]+)\//.test(id))
            return "vendor-antd";
          return undefined;
        },
      },
    },
  },
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
        "src/test/setup.ts",
        "src/test/mockApi.ts",
        "src/test/render.tsx",
        "src/contract/generated.ts",
      ],
      // Floor 70 % (docs/context-budget.md). Raise, never lower.
      thresholds: { statements: 70, branches: 70, functions: 70, lines: 70 },
    },
  },
});
