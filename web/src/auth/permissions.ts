import {
  type Permission,
  type PermissionSnapshot,
  type ResourceType,
  TAXONOMY,
} from "../access/plan03-types";

function parse(permission: Permission): { resource: ResourceType; action: string } | undefined {
  const [resource, action] = permission.split(":");
  if (!resource || !action || !(resource in TAXONOMY)) return undefined;
  const actions: readonly string[] = TAXONOMY[resource as ResourceType].actions;
  if (!actions.includes(action)) return undefined;
  return { resource: resource as ResourceType, action };
}

/**
 * The one place a grant is read. Components ask for a permission, never for a role.
 *
 * Admin allows everything. Otherwise a grant must match: org-scoped resources match a grant with
 * a null workspace; workspace-scoped ones match a grant for `workspaceId` or an org default (null,
 * every workspace). A workspace-scoped permission with no `workspaceId` is false here: the
 * `usePermission` hook fills in the active workspace. An unknown or missing snapshot is false.
 */
export function hasPermission(
  snapshot: PermissionSnapshot | undefined,
  permission: Permission,
  workspaceId?: string | null,
): boolean {
  if (!snapshot) return false;
  if (snapshot.admin) return true;
  const p = parse(permission);
  if (!p) return false;
  const { scope } = TAXONOMY[p.resource];
  if (scope === "workspace" && !workspaceId) return false;
  return snapshot.grants.some(
    (g) =>
      g.resourceType === p.resource &&
      g.action === p.action &&
      (g.workspaceId === null || (scope === "workspace" && g.workspaceId === workspaceId)),
  );
}

/**
 * The gateway's PermAdmin routes (role changes, group writes, org defaults): no grant confers
 * them. The snapshot carries `admin`, so the UI still never reads a role.
 */
export function isAdminSnapshot(snapshot: PermissionSnapshot | undefined): boolean {
  return snapshot?.admin === true;
}
