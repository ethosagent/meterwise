import { Result, Tabs } from "antd";
import { useIsAdmin, usePermission } from "../../auth/usePermission";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { InvitationsTable } from "../organization/InvitationsTable";
import { GroupsTab } from "./GroupsTab";
import { OrgDefaultsTab } from "./OrgDefaultsTab";
import { UsersTab } from "./UsersTab";

/** Settings > Access management: Groups, Users, Defaults, Invitations. Each tab appears only when its reads would not 403. */
export function AccessManagementPage() {
  const canRead = usePermission("member:read");
  const canManage = usePermission("member:manage");
  const isAdmin = useIsAdmin();
  const tabs = [
    ...(canRead
      ? [
          { key: "groups", label: "Groups", children: <GroupsTab isAdmin={isAdmin} /> },
          { key: "users", label: "Users", children: <UsersTab /> },
          { key: "defaults", label: "Defaults", children: <OrgDefaultsTab isAdmin={isAdmin} /> },
        ]
      : []),
    ...(canManage
      ? [{ key: "invitations", label: "Invitations", children: <InvitationsTable /> }]
      : []),
  ];
  return (
    <section aria-labelledby="page-title">
      <PageHead page={pageByKey("access")} />
      {tabs.length === 0 ? (
        <Result
          status="403"
          title="You do not have access to this page"
          subTitle="Ask an organization admin if you need it."
        />
      ) : (
        <Tabs items={tabs} />
      )}
    </section>
  );
}
