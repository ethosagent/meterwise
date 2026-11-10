import { Alert, Button, Card, Result, Skeleton } from "antd";
import type { ReactNode } from "react";
import type { PermissionName as Permission } from "../access/taxonomy";
import { usePermissionSnapshot } from "../api/access";
import { useActiveWorkspace } from "../api/identity";
import { usePermission } from "../auth/usePermission";
import type { Workspace } from "../contract/generated";
import { NoWorkspaceAccess } from "./NoWorkspaceAccess";

/** The page body when the caller lacks `permission` in the workspace: an explanation, and no request is made. */
export function NoAccess({ what, permission }: { what: string; permission: Permission }) {
  return (
    <Card>
      <Result
        status="403"
        title={`You don't have access to ${what}`}
        subTitle={`This needs the ${permission} permission in this workspace. Ask an admin to grant it.`}
      />
    </Card>
  );
}

/**
 * Resolves the active workspace and the caller's `permission` in it before rendering `children`.
 * While either is loading it shows a skeleton; without the permission it shows NoAccess. A page's
 * queries live inside `children`, so a denied caller never triggers a request.
 */
export function WorkspaceGuard({
  permission,
  what,
  children,
}: {
  permission: Permission;
  what: string;
  children: (ws: Workspace) => ReactNode;
}) {
  const active = useActiveWorkspace();
  const snapshot = usePermissionSnapshot();
  const ws = active.state === "ready" ? active.workspace : null;
  const allowed = usePermission(permission, ws?.id ?? null);
  if (active.state === "none") return <NoWorkspaceAccess />;
  if (active.state === "missing") {
    return (
      <Alert
        type="error"
        showIcon
        title="Your active workspace could not be found"
        action={<Button onClick={active.retry}>Try again</Button>}
      />
    );
  }
  if (ws === null || snapshot.isPending) return <Skeleton active />;
  if (!allowed) return <NoAccess what={what} permission={permission} />;
  return <>{children(ws)}</>;
}
