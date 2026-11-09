import { describe, expect, it } from "vitest";
import { ADMIN_ME, MEMBER_ME } from "../test/mockApi";
import { hasPermission } from "./permissions";

describe("hasPermission", () => {
  it("lets any member create a workspace, but not rename or delete one", () => {
    expect(hasPermission(MEMBER_ME, "workspaces.create")).toBe(true);
    expect(hasPermission(MEMBER_ME, "workspaces.manage")).toBe(false);
    expect(hasPermission(MEMBER_ME, "invitations.manage")).toBe(false);
  });

  it("gives an admin every workspace and invitation capability", () => {
    expect(hasPermission(ADMIN_ME, "workspaces.create")).toBe(true);
    expect(hasPermission(ADMIN_ME, "workspaces.manage")).toBe(true);
    expect(hasPermission(ADMIN_ME, "invitations.manage")).toBe(true);
  });

  it("grants nothing before the caller is known", () => {
    expect(hasPermission(undefined, "workspaces.create")).toBe(false);
  });
});
