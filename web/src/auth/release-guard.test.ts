import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const SCRIPT = join(process.cwd(), "scripts", "assert-no-mock.mjs");
const dirs: string[] = [];

function buildDir(files: Record<string, string>): string {
  const dir = mkdtempSync(join(tmpdir(), "no-mock-"));
  dirs.push(dir);
  mkdirSync(join(dir, "assets"));
  for (const [name, text] of Object.entries(files)) writeFileSync(join(dir, name), text);
  return dir;
}

const run = (dir: string) => spawnSync("node", [SCRIPT, dir], { encoding: "utf8" });

afterEach(() => {
  for (const d of dirs.splice(0)) rmSync(d, { recursive: true, force: true });
});

describe("assert-no-mock (the release build guard)", () => {
  it("passes a bundle without the mock", () => {
    expect(run(buildDir({ "assets/app.js": "console.log('hi')" })).status).toBe(0);
  });

  it("fails a bundle that contains the mock identity provider, naming the file", () => {
    const r = run(
      buildDir({ "assets/app.js": "x='Development sign-in (mock identity provider)'" }),
    );
    expect(r.status).toBe(1);
    expect(r.stderr).toContain("assets/app.js");
  });

  it("fails on the mock's storage key too", () => {
    expect(
      run(buildDir({ "assets/a.js": "sessionStorage.getItem('auth.mock.email')" })).status,
    ).toBe(1);
  });

  it("refuses an empty build rather than calling it clean", () => {
    expect(run(buildDir({})).status).toBe(1);
  });
});
