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

/** Test-support files that are not shipped; anything else under src/test/ is shipped code. */
const TEST_SUPPORT = new Set(["../test/setup.ts", "../test/mockApi.ts", "../test/render.tsx"]);
const GENERATED = "../contract/generated.ts";

/** Whether a source (path relative to src, as "../x") is shipped and so subject to the scans. */
function isShipped(p: string): boolean {
  return (
    !p.includes(".test.") && !TEST_SUPPORT.has(p) && p !== GENERATED && p !== "../theme/theme.ts"
  );
}

const shipped = Object.entries(sources).filter(([p]) => isShipped(p));

describe("design-token and brand hygiene", () => {
  it("exempts only the named test-support files, generated.ts and theme.ts", () => {
    expect(isShipped("../test/extra.ts")).toBe(true); // a new file in src/test/ is scanned
    expect(isShipped("../contract/hand.ts")).toBe(true); // a hand-written file next to generated.ts is scanned
    expect(isShipped("../contract/contract.test.ts")).toBe(false);
    expect(isShipped("../test/mockApi.ts")).toBe(false);
    expect(isShipped("../contract/generated.ts")).toBe(false);
    expect(isShipped("../theme/theme.ts")).toBe(false);
    expect(isShipped("../shell/Shell.tsx")).toBe(true);
  });

  it("the exempt files all exist (a rename must not widen the exemption silently)", () => {
    for (const p of [...TEST_SUPPORT, GENERATED]) expect(Object.keys(sources)).toContain(p);
  });

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
