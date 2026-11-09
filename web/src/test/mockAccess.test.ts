import { describe, expect, it } from "vitest";
import { createAccessMock } from "./mockAccess";

const g = (resourceType: string, action: string, workspaceId: string | null) => ({
  workspaceId,
  resourceType,
  action,
});

function mockWith(grants: ReturnType<typeof g>[]) {
  return createAccessMock(false, { snapshot: { admin: false, grants } as never });
}

// The mock's `can` must follow the taxonomy's scope rule, as the Go evaluator does.
describe("createAccessMock can()", () => {
  it("an org-scoped resource is allowed only by a grant with no workspace", () => {
    expect(mockWith([g("member", "read", null)]).can("member", "read")).toBe(true);
    expect(mockWith([g("member", "read", "ws_1")]).can("member", "read", "ws_1")).toBe(false);
  });

  it("a workspace argument neither widens nor narrows an org-scoped check", () => {
    expect(mockWith([g("member", "read", null)]).can("member", "read", "ws_1")).toBe(true);
    expect(mockWith([g("member", "read", "ws_1")]).can("member", "read", "ws_1")).toBe(false);
  });

  it("a workspace-scoped resource matches a null grant anywhere and a pinned grant only there", () => {
    const pinned = mockWith([g("session", "read", "ws_1")]);
    expect(pinned.can("session", "read", "ws_1")).toBe(true);
    expect(pinned.can("session", "read", "ws_2")).toBe(false);
    expect(mockWith([g("session", "read", null)]).can("session", "read", "ws_2")).toBe(true);
  });

  it("an action outside the taxonomy is never allowed to a non-admin", () => {
    expect(mockWith([g("session", "write", null)]).can("session", "write")).toBe(false);
  });
});
