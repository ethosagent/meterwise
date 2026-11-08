#!/usr/bin/env bash
# new-page.sh <name> <permission> — create web/src/features/<name>/ with the five files.
# Needs React in web/node_modules (installed by the web shell plan).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
name="${1:?usage: new-page.sh <name> <permission>}" perm="${2:?permission, e.g. workspace:read}"
[[ $name =~ ^[a-z][a-z0-9-]*$ ]] || { echo "✗ name must be kebab-case" >&2; exit 1; }
[ -d web/node_modules/react ] || { echo "✗ React is not installed yet (the web shell plan adds it); nothing created" >&2; exit 1; }
dir="web/src/features/$name"
[ ! -e "$dir" ] || { echo "✗ $dir exists" >&2; exit 1; }
mkdir -p "$dir"
comp="$(printf '%s' "$name" | awk -F- '{for(i=1;i<=NF;i++) printf toupper(substr($i,1,1)) substr($i,2)}')"
cat >"$dir/api.ts" <<TS
import type { ErrorBody } from "../../contract/generated";

/** Calls for the $name page. Add one typed function per endpoint it uses. */
export async function ping(signal?: AbortSignal): Promise<ErrorBody | null> {
  const res = await fetch("/healthz", signal ? { signal } : {});
  return res.ok ? null : ((await res.json()) as ErrorBody);
}
TS
cat >"$dir/hooks.ts" <<TS
import { useQuery } from "@tanstack/react-query";
import { ping } from "./api";

export const use${comp}Ping = () => useQuery({ queryKey: ["$name", "ping"], queryFn: ({ signal }) => ping(signal) });
TS
cat >"$dir/Page.tsx" <<TSX
import { use${comp}Ping } from "./hooks";

/** Requires permission "$perm". */
export function ${comp}Page() {
  const q = use${comp}Ping();
  return <section aria-busy={q.isPending}>$name</section>;
}
TSX
cat >"$dir/Page.test.tsx" <<TSX
import { describe, expect, it } from "vitest";
import { ${comp}Page } from "./Page";

describe("${comp}Page", () => {
  it("is a component", () => expect(typeof ${comp}Page).toBe("function"));
});
TSX
printf '# %s\n\nPermission: `%s`.\n\n## Shows\n\n## Endpoints\n' "$name" "$perm" >"$dir/README.md"
echo "created $dir (register the route, then replace the sample ping with the real endpoint)"
