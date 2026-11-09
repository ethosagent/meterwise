import { describe, expect, it } from "vitest";
import type { Grant, PermissionSnapshot } from "../access/plan03-types";
import { hasPermission, isAdminSnapshot } from "./permissions";

const grant = (
  workspaceId: string | null,
  resourceType: Grant["resourceType"],
  action: Grant["action"],
): Grant => ({
  workspaceId,
  resourceType,
  action,
});
const ADMIN: PermissionSnapshot = { admin: true, grants: [] };
const member = (...grants: Grant[]): PermissionSnapshot => ({ admin: false, grants });

describe("hasPermission", () => {
  it("lets an admin with zero grant rows do everything, in any workspace", () => {
    expect(hasPermission(ADMIN, "workspace:manage", "ws_9")).toBe(true);
    expect(hasPermission(ADMIN, "member:manage")).toBe(true);
    expect(isAdminSnapshot(ADMIN)).toBe(true);
  });

  it("grants a member only what a grant says, in the grant's workspace only", () => {
    const s = member(grant("ws_1", "workspace", "write"));
    expect(hasPermission(s, "workspace:write", "ws_1")).toBe(true);
    expect(hasPermission(s, "workspace:write", "ws_2")).toBe(false);
    expect(hasPermission(s, "workspace:manage", "ws_1")).toBe(false);
    expect(isAdminSnapshot(s)).toBe(false);
  });

  it("matches an org default (null workspace) in every workspace", () => {
    const s = member(grant(null, "session", "read"));
    expect(hasPermission(s, "session:read", "ws_1")).toBe(true);
    expect(hasPermission(s, "session:read", "ws_2")).toBe(true);
  });

  it("needs a workspace for a workspace-scoped permission, not for an org-scoped one", () => {
    const s = member(grant(null, "session", "read"), grant(null, "member", "read"));
    expect(hasPermission(s, "session:read")).toBe(false);
    expect(hasPermission(s, "session:read", null)).toBe(false);
    expect(hasPermission(s, "member:read")).toBe(true);
    expect(hasPermission(s, "member:manage")).toBe(false);
  });

  it("does not let a workspace grant satisfy an org-scoped resource", () => {
    expect(hasPermission(member(grant("ws_1", "member", "read")), "member:read", "ws_1")).toBe(
      false,
    );
  });

  it("rejects an action the taxonomy does not define for that resource, even with a matching grant", () => {
    const odd = member({ workspaceId: null, resourceType: "session", action: "write" });
    expect(hasPermission(odd, "session:write" as never, "ws_1")).toBe(false);
    expect(
      hasPermission(member(grant(null, "session", "read")), "session:manage" as never, "ws_1"),
    ).toBe(false);
  });

  it("is false for a missing snapshot and an unknown permission", () => {
    expect(hasPermission(undefined, "workspace:read", "ws_1")).toBe(false);
    expect(hasPermission(member(), "nope:read" as never, "ws_1")).toBe(false);
    expect(hasPermission(member(), "workspace" as never, "ws_1")).toBe(false);
    expect(isAdminSnapshot(undefined)).toBe(false);
  });
});
