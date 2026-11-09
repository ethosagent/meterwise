import { describe, expect, it } from "vitest";
import { AUDIT_ACTIONS } from "../../contract/generated";
import { ACTION_OPTIONS } from "./AuditLogTab";

// The fixed Plan 04 action list (04-interface.md). The generated list comes from contract.AuditActions();
// this pin fails when either side adds, drops or reorders an action without the other.
const PLAN_04_ACTIONS = [
  "workspace.create",
  "workspace.update",
  "workspace.delete",
  "workspace.url_reset",
  "workspace.url_retire_previous",
  "workspace.allowed_cidrs_update",
  "invitation.create",
  "invitation.resend",
  "invitation.revoke",
  "member.role_change",
  "member.remove",
  "group.create",
  "group.update",
  "group.delete",
  "group.permissions_update",
  "group.workspace_attach",
  "group.workspace_detach",
  "group.member_add",
  "group.member_remove",
  "group.members_update",
  "org.permissions_update",
];

describe("audit action list", () => {
  it("the generated list is the Plan 04 list", () => {
    expect([...AUDIT_ACTIONS]).toEqual(PLAN_04_ACTIONS);
  });
  it("the filter offers exactly the generated actions", () => {
    expect(ACTION_OPTIONS.map((o) => o.value)).toEqual([...AUDIT_ACTIONS]);
  });
});
