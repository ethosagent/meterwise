import { Card, Result } from "antd";

/** Shown in place of a workspace page when the caller can read no workspace (Me.activeWorkspaceId is null). */
export function NoWorkspaceAccess() {
  return (
    <Card>
      <Result
        status="info"
        title="You do not have access to any workspace"
        subTitle="Ask an organization admin to add you to a group with a workspace, or to give your organization access by default."
      />
    </Card>
  );
}
