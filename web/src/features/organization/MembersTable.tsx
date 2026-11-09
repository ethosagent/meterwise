import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Card, Flex, Popconfirm, Select, Table, Tag, Typography, theme } from "antd";
import { useState } from "react";
import { useMembers, useRemoveMember, useSetMemberRole } from "../../api/access";
import { useInvitations, useResendInvitation, useRevokeInvitation } from "../../api/identity";
import { useAccessToast } from "../../api/useAccessToast";
import type { Invitation, Member, Role } from "../../contract/generated";
import { InviteModal } from "./InviteModal";

type Row =
  | { key: string; kind: "member"; member: Member }
  | { key: string; kind: "invite"; invitation: Invitation };

const ROLE_OPTIONS: { value: Role; label: string }[] = [
  { value: "member", label: "Member" },
  { value: "admin", label: "Admin" },
];

interface Props {
  /** member:manage: invite, and resend or revoke a pending invitation. */
  canManage: boolean;
  /** PermAdmin: change a role, remove a member. */
  isAdmin: boolean;
}

/** Settings > Organization > Members: active members and pending invitations in one table. */
export function MembersTable({ canManage, isAdmin }: Props) {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const members = useMembers(true);
  const invitations = useInvitations(canManage);
  const setRole = useSetMemberRole();
  const remove = useRemoveMember();
  const resend = useResendInvitation();
  const revoke = useRevokeInvitation();
  const [inviting, setInviting] = useState(false);
  const fail = useAccessToast();

  const rows: Row[] = [
    ...(members.data ?? []).map((m): Row => ({ key: m.userId, kind: "member", member: m })),
    ...(invitations.data ?? [])
      .filter((i) => i.status === "pending")
      .map((i): Row => ({ key: i.id, kind: "invite", invitation: i })),
  ];

  return (
    <Card>
      <Flex
        wrap
        justify="space-between"
        align="center"
        gap={token.marginSM}
        style={{ marginBottom: token.marginMD }}
      >
        <Typography.Title level={4} style={{ margin: 0 }}>
          Members
        </Typography.Title>
        {canManage ? (
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setInviting(true)}>
            Invite member
          </Button>
        ) : null}
      </Flex>
      {canManage ? <InviteModal open={inviting} onClose={() => setInviting(false)} /> : null}
      <Table<Row>
        rowKey="key"
        loading={members.isLoading}
        dataSource={rows}
        pagination={false}
        scroll={{ x: "max-content" }}
        locale={{ emptyText: members.isError ? "Could not load members." : "No members yet." }}
        columns={[
          {
            title: "Member",
            key: "who",
            render: (_: unknown, r) =>
              r.kind === "member" ? (
                <>
                  <Typography.Text>{r.member.name}</Typography.Text>
                  <br />
                  <Typography.Text type="secondary">{r.member.email}</Typography.Text>
                </>
              ) : (
                <Typography.Text>{r.invitation.email}</Typography.Text>
              ),
          },
          {
            title: "Role",
            key: "role",
            render: (_: unknown, r) =>
              r.kind === "member" && isAdmin ? (
                <Select<Role>
                  aria-label={`Role for ${r.member.email}`}
                  value={r.member.role}
                  options={ROLE_OPTIONS}
                  style={{ minWidth: token.controlHeightLG * 3 }}
                  loading={setRole.isPending && setRole.variables?.userId === r.member.userId}
                  onChange={(role) =>
                    setRole.mutate(
                      { userId: r.member.userId, role },
                      { onError: (e) => fail(e, "Could not change the role.") },
                    )
                  }
                />
              ) : (
                <Tag>{r.kind === "member" ? r.member.role : r.invitation.role}</Tag>
              ),
          },
          {
            title: "Status",
            key: "status",
            render: (_: unknown, r) =>
              r.kind === "invite" ? (
                <Tag color="processing">Pending invite</Tag>
              ) : (
                <Tag color="success">Active</Tag>
              ),
          },
          {
            title: "",
            key: "actions",
            render: (_: unknown, r) => {
              if (r.kind === "invite") {
                const inv = r.invitation;
                if (!canManage) return null;
                // Only an admin may touch an admin invitation; the escalation guard would answer 403.
                const locked = inv.role === "admin" && !isAdmin;
                return (
                  <>
                    <Button
                      type="link"
                      disabled={locked}
                      loading={resend.isPending && resend.variables === inv.id}
                      onClick={() =>
                        resend.mutate(inv.id, {
                          onSuccess: () => message.success(`Invitation resent to ${inv.email}`),
                          onError: (e) => fail(e, "Could not resend the invitation."),
                        })
                      }
                    >
                      Resend
                    </Button>
                    <Popconfirm
                      title={`Revoke the invitation for ${inv.email}?`}
                      okText="Revoke"
                      okButtonProps={{ danger: true }}
                      onConfirm={() =>
                        revoke.mutate(inv.id, {
                          onError: (e) => fail(e, "Could not revoke the invitation."),
                        })
                      }
                    >
                      <Button type="link" danger disabled={locked}>
                        Revoke
                      </Button>
                    </Popconfirm>
                  </>
                );
              }
              if (!isAdmin) return null;
              return (
                <Popconfirm
                  title={`Remove ${r.member.email} from the organization?`}
                  description="They lose access immediately and leave every group."
                  okText="Remove"
                  okButtonProps={{ danger: true }}
                  onConfirm={() =>
                    remove.mutate(r.member.userId, {
                      onError: (e) => fail(e, "Could not remove the member."),
                    })
                  }
                >
                  <Button type="link" danger>
                    Remove
                  </Button>
                </Popconfirm>
              );
            },
          },
        ]}
      />
    </Card>
  );
}
