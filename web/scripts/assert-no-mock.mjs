// A release bundle must not contain the dev-only mock identity provider. This scans a build directory
// for strings that exist only in src/auth/mock.tsx and exits 1 if any is found. Run by `build:release`.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const MARKERS = ["Development sign-in (mock identity provider)", "auth.mock.email"];

function* files(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) yield* files(p);
    else if (/\.(js|mjs|css|html)$/.test(name)) yield p;
  }
}

const dir = process.argv[2] ?? "dist";
let scanned = 0;
const found = [];
for (const f of files(dir)) {
  scanned++;
  const text = readFileSync(f, "utf8");
  for (const m of MARKERS) if (text.includes(m)) found.push(`${f}: ${m}`);
}
if (scanned === 0) {
  console.error(`assert-no-mock: no files under ${dir}; refusing to call an empty build clean.`);
  process.exit(1);
}
if (found.length > 0) {
  console.error(`assert-no-mock: the mock identity provider is in the build:\n${found.join("\n")}`);
  process.exit(1);
}
console.log(`assert-no-mock: ${scanned} files scanned, no mock identity provider.`);
