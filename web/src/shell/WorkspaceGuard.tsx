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
 * Renders the none / missing / loading states of the active workspace and, once it is ready,
 * `children(workspace)`. The one place those three branches are written.
 */
export function ActiveWorkspaceGate({
  active,
  missingHint,
  children,
}: {
  active: ReturnType<typeof useActiveWorkspace>;
  /** Extra text under the "could not be found" alert. */
  missingHint?: string;
  children: (ws: Workspace) => ReactNode;
}) {
  if (active.state === "none") return <NoWorkspaceAccess />;
  if (active.state === "missing") {
    return (
      <Alert
        type="error"
        showIcon
        title="Your active workspace could not be found"
        description={missingHint}
        action={<Button onClick={active.retry}>Try again</Button>}
      />
    );
  }
  if (active.state === "loading") return <Skeleton active />;
  return <>{children(active.workspace)}</>;
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
  const allowed = usePermission(permission, active.state === "ready" ? active.workspace.id : null);
  return (
    <ActiveWorkspaceGate active={active}>
      {(ws) => {
        if (snapshot.isPending) return <Skeleton active />;
        if (!allowed) return <NoAccess what={what} permission={permission} />;
        return <>{children(ws)}</>;
      }}
    </ActiveWorkspaceGate>
  );
}
