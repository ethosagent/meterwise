import { Table, Tag } from "antd";
import { useGroups, useMembers } from "../../api/access";
import type { Member } from "../../contract/generated";

/** Read-only: who is in the organization and which groups they belong to. Roles change on Settings > Organization. */
export function UsersTab() {
  const members = useMembers(true);
  const groups = useGroups(true);
  const nameOf = (id: string) => groups.data?.find((g) => g.id === id)?.name ?? id;
  return (
    <Table<Member>
      rowKey="userId"
      loading={members.isLoading}
      dataSource={members.data ?? []}
      pagination={false}
      scroll={{ x: "max-content" }}
      locale={{ emptyText: members.isError ? "Could not load users." : "No users yet." }}
      columns={[
        { title: "Name", dataIndex: "name" },
        { title: "Email", dataIndex: "email" },
        { title: "Role", dataIndex: "role", render: (r: string) => <Tag>{r}</Tag> },
        {
          title: "Groups",
          key: "groups",
          render: (_: unknown, m) => m.groupIds.map((id) => <Tag key={id}>{nameOf(id)}</Tag>),
        },
        {
          title: "Joined",
          dataIndex: "joinedAt",
          render: (v: string) => new Date(v).toLocaleDateString(),
        },
      ]}
    />
  );
}
