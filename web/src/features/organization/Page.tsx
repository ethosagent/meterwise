import { Card, Descriptions, theme } from "antd";
import { useConfig } from "../../api/config";
import { useMe } from "../../api/identity";
import { hasPermission } from "../../auth/permissions";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { InvitationsTable } from "./InvitationsTable";

/** Settings > Organization: the one organization's name, and invitations for admins. */
export function OrganizationPage() {
  const { token } = theme.useToken();
  const me = useMe().data;
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
      {hasPermission(me, "invitations.manage") ? (
        <div style={{ marginTop: token.marginLG }}>
          <InvitationsTable />
        </div>
      ) : null}
    </section>
  );
}
