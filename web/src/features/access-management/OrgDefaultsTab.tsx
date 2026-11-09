import { Alert, Skeleton, theme } from "antd";
import { useOrgDefaults, usePutOrgDefaults } from "../../api/access";
import { GrantsEditor } from "./GrantsEditor";
import type { MatrixSection } from "./PermissionsMatrix";

const SECTIONS: MatrixSection[] = [
  { title: "Every workspace", workspaceId: null, scope: "workspace" },
  { title: "Organization", workspaceId: null, scope: "org" },
];

/** Grants every member holds, in every workspace (a null workspace). Readable with member:read; editable by admins. */
export function OrgDefaultsTab({ isAdmin }: { isAdmin: boolean }) {
  const { token } = theme.useToken();
  const defaults = useOrgDefaults(true);
  const put = usePutOrgDefaults();
  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: token.marginMD }}
        title="Organization defaults apply to all members in all workspaces"
        description="Groups add to these grants; they never take one away. Removing a default removes it for everyone who does not get it from a group."
      />
      {defaults.data ? (
        <GrantsEditor
          // Remount when the server's copy changes, so a refetch replaces a stale draft.
          key={JSON.stringify(defaults.data)}
          sections={SECTIONS}
          saved={defaults.data}
          disabled={!isAdmin}
          saving={put.isPending}
          save={(grants, on) => put.mutate(grants, on)}
        />
      ) : defaults.isError ? (
        <Alert type="error" showIcon title="Could not load the organization defaults." />
      ) : (
        <Skeleton active />
      )}
    </>
  );
}
