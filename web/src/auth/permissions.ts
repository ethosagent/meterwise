import type { Me } from "../contract/generated";

/**
 * Capabilities the UI gates on. Plan 03 replaces the table below with grants from the server.
 * Ruling: any member may create a workspace; rename and delete are admin-only. This mirrors the
 * permission table in internal/contract/routes.go.
 */
export type Permission = "invitations.manage" | "workspaces.create" | "workspaces.manage";

const ADMIN_ONLY: readonly Permission[] = ["invitations.manage", "workspaces.manage"];
const ANY_MEMBER: readonly Permission[] = ["workspaces.create"];

/** The one place a role is read. Components ask for a permission, never for a role. */
export function hasPermission(me: Me | undefined, permission: Permission): boolean {
  if (!me) return false;
  if (ANY_MEMBER.includes(permission)) return me.role === "admin" || me.role === "member";
  return me.role === "admin" && ADMIN_ONLY.includes(permission);
}
