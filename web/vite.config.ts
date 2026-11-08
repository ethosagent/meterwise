import { defineConfig } from "vitest/config";

// Plan 01 adds the React plugin; the shell here builds and tests without it.
export default defineConfig({
  build: { outDir: "dist", sourcemap: true },
  test: {
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts", "src/**/*.tsx"],
      exclude: [
        "src/**/*.test.ts",
        "src/**/*.test.tsx",
        "src/main.ts",
        "src/contract/generated.ts",
      ],
      // Floor 70 % (docs/context-budget.md). Raise, never lower.
      thresholds: { statements: 70, branches: 70, functions: 70, lines: 70 },
    },
  },
});
