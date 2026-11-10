import { describe, expect, it } from "vitest";
import type { PermissionName } from "../access/taxonomy";
import type { Grant, PermissionSnapshot } from "../contract/generated";
import fixture from "../test/fixtures/savingsPermCases.json";
import { hasPermission } from "./permissions";

// The same file drives internal/api/savings_perm_test.go: the gateway and hasPermission must agree on every case.
const WS: Record<string, string> = { A: "ws_a", B: "ws_b" };
const PERMS = fixture.routes as Record<string, string>;

describe("savingsPermCases.json (shared with the Go gateway test)", () => {
  for (const c of fixture.cases) {
    it(c.name, () => {
      const grants: Grant[] = c.grants.map((g) => ({
        workspaceId: g.workspace === null ? null : (WS[g.workspace] ?? g.workspace),
        resourceType: g.resourceType as Grant["resourceType"],
        action: g.action as Grant["action"],
      }));
      const snapshot: PermissionSnapshot = { admin: c.admin, grants };
      const perm = PERMS[c.route] as PermissionName;
      expect(hasPermission(snapshot, perm, WS[c.workspace])).toBe(c.allowed);
    });
  }
});
