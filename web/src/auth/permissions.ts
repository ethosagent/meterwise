import type { Me } from "../api/identity-types";

/** Capabilities the UI gates on. Plan 03 replaces the table below with grants from the server. */
export type Permission = "invitations.manage" | "workspaces.manage";

const ADMIN_ONLY: readonly Permission[] = ["invitations.manage", "workspaces.manage"];

/** The one place a role is read. Components ask for a permission, never for a role. */
export function hasPermission(me: Me | undefined, permission: Permission): boolean {
  return me?.role === "admin" && ADMIN_ONLY.includes(permission);
}
