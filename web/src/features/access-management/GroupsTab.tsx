import { PlusOutlined } from "@ant-design/icons";
import { Button, Flex, Form, Input, Table, theme } from "antd";
import { useState } from "react";
import { ACCESS_ERROR_TEXT, useCreateGroup, useGroups } from "../../api/access";
import { describeApiError } from "../../api/errors";
import type { Group } from "../../contract/generated";
import { MutationFormModal } from "../../shell/MutationFormModal";
import { GroupDrawer } from "./GroupDrawer";

function NewGroupModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useCreateGroup();
  return (
    <MutationFormModal<{ name: string }>
      open={open}
      onClose={onClose}
      name="newGroup"
      title="New group"
      submitText="Create group"
      mutation={create}
      errorText={(e) =>
        describeApiError(e, {
          byCode: ACCESS_ERROR_TEXT,
          fallback: "Could not create the group. Try again.",
        })
      }
      onSubmit={(v, done) => create.mutate(v.name.trim(), { onSuccess: done })}
    >
      <Form.Item
        label="Name"
        name="name"
        rules={[{ required: true, whitespace: true, message: "Enter a name" }]}
      >
        <Input maxLength={80} autoComplete="off" />
      </Form.Item>
    </MutationFormModal>
  );
}

/** Groups table; a row opens the group Drawer. Creating is admin-only, reading needs member:read. */
export function GroupsTab({ isAdmin }: { isAdmin: boolean }) {
  const { token } = theme.useToken();
  const groups = useGroups(true);
  const [openId, setOpenId] = useState<string | undefined>();
  const [creating, setCreating] = useState(false);
  const open = groups.data?.find((g) => g.id === openId);
  return (
    <>
      <Flex justify="end" style={{ marginBottom: token.marginMD }}>
        {isAdmin ? (
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
            New group
          </Button>
        ) : null}
      </Flex>
      <NewGroupModal open={creating} onClose={() => setCreating(false)} />
      <Table<Group>
        rowKey="id"
        loading={groups.isLoading}
        dataSource={groups.data ?? []}
        pagination={false}
        scroll={{ x: "max-content" }}
        locale={{ emptyText: groups.isError ? "Could not load groups." : "No groups yet." }}
        columns={[
          {
            title: "Name",
            dataIndex: "name",
            render: (n: string, g) => (
              <Button type="link" onClick={() => setOpenId(g.id)}>
                {n}
              </Button>
            ),
          },
          { title: "Members", dataIndex: "memberCount" },
          { title: "Workspaces", key: "ws", render: (_: unknown, g) => g.workspaceIds.length },
        ]}
      />
      <GroupDrawer group={open} onClose={() => setOpenId(undefined)} isAdmin={isAdmin} />
    </>
  );
}
