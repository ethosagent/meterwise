import { describe, expect, it } from "vitest";

// Sources as raw text, bundled by Vite: no node built-ins in web/src.
const sources = import.meta.glob(["../**/*.ts", "../**/*.tsx", "../**/*.css"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const shipped = Object.entries(sources).filter(
  ([p]) =>
    !p.includes(".test.") &&
    !p.includes("/test/") &&
    !p.includes("/contract/") &&
    !p.endsWith("/theme/theme.ts"),
);

describe("design-token and brand hygiene", () => {
  it("scans a meaningful set of sources", () => {
    expect(shipped.length).toBeGreaterThan(20);
  });

  it("holds no hex colour outside theme.ts", () => {
    const hits = shipped.filter(([, src]) => /#[0-9a-fA-F]{3,8}\b/.test(src)).map(([p]) => p);
    expect(hits).toEqual([]);
  });

  it("hardcodes no product name (the brand comes from /api/config)", () => {
    const hits = shipped.filter(([, src]) => /meterwise/i.test(src)).map(([p]) => p);
    expect(hits).toEqual([]);
  });
});
