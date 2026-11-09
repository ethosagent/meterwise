import { PlusOutlined } from "@ant-design/icons";
import { Alert, Button, Flex, Form, Input, Modal, Table, theme } from "antd";
import { useState } from "react";
import type { Group } from "../../access/plan03-types";
import { ACCESS_ERROR_TEXT, useCreateGroup, useGroups } from "../../api/access";
import { describeApiError } from "../../api/errors";
import { GroupDrawer } from "./GroupDrawer";

function NewGroupModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useCreateGroup();
  const [form] = Form.useForm<{ name: string }>();
  const close = () => {
    create.reset();
    form.resetFields();
    onClose();
  };
  return (
    <Modal
      open={open}
      title="New group"
      onCancel={close}
      destroyOnHidden
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="primary" loading={create.isPending} onClick={() => form.submit()}>
            Create group
          </Button>
        </>
      }
    >
      <Form
        name="newGroup"
        form={form}
        layout="vertical"
        requiredMark={false}
        onFinish={(v) => create.mutate(v.name.trim(), { onSuccess: close })}
      >
        {create.isError ? (
          <Alert
            type="error"
            showIcon
            title={describeApiError(create.error, {
              byCode: ACCESS_ERROR_TEXT,
              fallback: "Could not create the group. Try again.",
            })}
          />
        ) : null}
        <Form.Item
          label="Name"
          name="name"
          rules={[{ required: true, whitespace: true, message: "Enter a name" }]}
        >
          <Input maxLength={80} autoComplete="off" />
        </Form.Item>
      </Form>
    </Modal>
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
