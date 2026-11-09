import { PlusOutlined } from "@ant-design/icons";
import { Button, Card, Flex, Table, Tag, Typography, theme } from "antd";
import { useState } from "react";
import { useInvitations } from "../../api/identity";
import type { Invitation } from "../../contract/generated";
import { InvitationActions } from "./InvitationActions";
import { InviteModal } from "./InviteModal";

const STATUS_COLOR: Record<Invitation["status"], string> = {
  pending: "processing",
  accepted: "success",
  revoked: "default",
  expired: "warning",
};

/**
 * Table of invitations with Resend and Revoke, for anyone with member:manage. Only an admin may touch an admin
 * invitation (the escalation guard answers 403 otherwise), so those two actions are disabled for everyone else.
 */
export function InvitationsTable({ isAdmin }: { isAdmin: boolean }) {
  const { token } = theme.useToken();
  const list = useInvitations(true);
  const [inviting, setInviting] = useState(false);

  return (
    <Card>
      {/* Not Card's title/extra: they truncate the title instead of wrapping on a narrow screen. */}
      <Flex
        wrap
        justify="space-between"
        align="center"
        gap={token.marginSM}
        style={{ marginBottom: token.marginMD }}
      >
        <Typography.Title level={4} style={{ margin: 0 }}>
          Invitations
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setInviting(true)}>
          Invite member
        </Button>
      </Flex>
      <InviteModal open={inviting} onClose={() => setInviting(false)} />
      <Table<Invitation>
        rowKey="id"
        loading={list.isLoading}
        dataSource={list.data ?? []}
        pagination={false}
        scroll={{ x: "max-content" }}
        locale={{ emptyText: list.isError ? "Could not load invitations." : "No invitations yet." }}
        columns={[
          { title: "Email", dataIndex: "email" },
          { title: "Role", dataIndex: "role" },
          {
            title: "Status",
            dataIndex: "status",
            render: (s: Invitation["status"]) => <Tag color={STATUS_COLOR[s]}>{s}</Tag>,
          },
          {
            title: "Expires",
            dataIndex: "expiresAt",
            render: (v: string) => new Date(v).toLocaleDateString(),
          },
          {
            title: "",
            key: "actions",
            render: (_: unknown, inv) =>
              inv.status === "pending" ? (
                <InvitationActions invitation={inv} isAdmin={isAdmin} />
              ) : null,
          },
        ]}
      />
    </Card>
  );
}
