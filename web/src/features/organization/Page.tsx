import { Card, Descriptions, theme } from "antd";
import { useConfig } from "../../api/config";
import { useIsAdmin, usePermission } from "../../auth/usePermission";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { InvitationsTable } from "./InvitationsTable";
import { MembersTable } from "./MembersTable";

/** Settings > Organization: the organization's name, and its members for anyone who may read them. */
export function OrganizationPage() {
  const { token } = theme.useToken();
  const canRead = usePermission("member:read");
  const canManage = usePermission("member:manage");
  const isAdmin = useIsAdmin();
  // The organization is named after the deployment (the seed uses APP_NAME), and the API has no org endpoint yet.
  const name = useConfig().data?.appName ?? "";
  return (
    <section aria-labelledby="page-title">
      <PageHead page={pageByKey("organization")} />
      <Card title="Organization">
        <Descriptions column={1}>
          <Descriptions.Item label="Name">{name}</Descriptions.Item>
        </Descriptions>
      </Card>
      {canRead || canManage ? (
        <div style={{ marginTop: token.marginLG }}>
          {canRead ? (
            <MembersTable canManage={canManage} isAdmin={isAdmin} />
          ) : (
            <InvitationsTable isAdmin={isAdmin} />
          )}
        </div>
      ) : null}
    </section>
  );
}
