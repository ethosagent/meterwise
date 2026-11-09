import { Alert, Space } from "antd";
import type { WorkspaceView } from "../../api/contract04";
import { usePermission } from "../../auth/usePermission";
import { AllowedCidrs } from "./AllowedCidrs";
import { ProxyUrl } from "./ProxyUrl";
import { ResetUrl } from "./ResetUrl";

/** Shown wherever the URL would be when the API sent it as null (no workspace_url:read). */
export function UrlHidden() {
  return (
    <Alert
      type="info"
      showIcon
      title="The proxy URL is hidden for your account"
      description="Ask an organization admin for the workspace URL, or for the access to see it."
    />
  );
}

/** The proxy URL with Copy and Reset URL: one component for Setup and Settings > Workspace. */
export function WorkspaceUrl({
  ws,
  withCidrs = false,
}: {
  ws: WorkspaceView;
  withCidrs?: boolean;
}) {
  const canManage = usePermission("workspace_url:manage", ws.id);
  if (ws.proxyUrl === null) return <UrlHidden />;
  return (
    <Space vertical size="middle" style={{ width: "100%" }}>
      <ProxyUrl url={ws.proxyUrl} />
      {canManage ? <ResetUrl workspaceId={ws.id} /> : null}
      {canManage && withCidrs && ws.allowedClientCidrs !== null ? (
        <AllowedCidrs workspaceId={ws.id} saved={ws.allowedClientCidrs} />
      ) : null}
    </Space>
  );
}
