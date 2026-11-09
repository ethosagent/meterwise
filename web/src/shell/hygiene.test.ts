import { readdirSync, readFileSync } from "node:fs";
import { join, relative, resolve, sep } from "node:path";
import { describe, expect, it } from "vitest";

// Sources are read with node:fs, not import.meta.glob("?raw"): a raw import registers the file as
// loaded, which hides never-imported files from the coverage floor.
const SRC = resolve(import.meta.dirname, "..");

function walk(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? walk(join(dir, e.name)) : [join(dir, e.name)],
  );
}

const sources: Record<string, string> = Object.fromEntries(
  walk(SRC)
    .filter((f) => /\.(ts|tsx|css)$/.test(f))
    .map((f) => [`../${relative(SRC, f).split(sep).join("/")}`, readFileSync(f, "utf8")]),
);

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
