import type { PermissionName as Permission } from "../access/taxonomy";
import { usePermissionSnapshot } from "../api/access";
import { useMe } from "../api/identity";
import { hasPermission, isAdminSnapshot } from "./permissions";

/**
 * Whether the caller may do `permission`, against the same snapshot the gateway evaluates.
 * Without a workspaceId, a workspace-scoped permission is checked against the active workspace.
 */
export function usePermission(permission: Permission, workspaceId?: string | null): boolean {
  const snapshot = usePermissionSnapshot().data;
  const active = useMe().data?.activeWorkspaceId;
  return hasPermission(snapshot, permission, workspaceId === undefined ? active : workspaceId);
}

/** For the routes the gateway marks PermAdmin. */
export function useIsAdmin(): boolean {
  return isAdminSnapshot(usePermissionSnapshot().data);
}

/** POST /api/workspaces is PermMember: any active membership, so a loaded Me is enough. */
export function useCanCreateWorkspace(): boolean {
  return useMe().data !== undefined;
}
