import {
  App,
  Button,
  Card,
  Form,
  Input,
  Popconfirm,
  Skeleton,
  Space,
  Typography,
  theme,
} from "antd";
import { useEffect } from "react";
import {
  useActiveWorkspace,
  useDeleteWorkspace,
  useMe,
  useRenameWorkspace,
} from "../../api/identity";
import type { Workspace } from "../../api/identity-types";
import { hasPermission } from "../../auth/permissions";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { ProxyUrl } from "./ProxyUrl";

function General({ ws, canManage }: { ws: Workspace; canManage: boolean }) {
  const { message } = App.useApp();
  const rename = useRenameWorkspace();
  const [form] = Form.useForm<{ name: string }>();
  useEffect(() => form.setFieldsValue({ name: ws.name }), [form, ws.name]);
  return (
    <Card title="General">
      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        disabled={!canManage}
        onFinish={(v) =>
          rename.mutate(
            { id: ws.id, name: v.name.trim() },
            {
              onSuccess: () => message.success("Workspace renamed"),
              onError: () => message.error("Could not rename the workspace."),
            },
          )
        }
      >
        <Form.Item
          label="Name"
          name="name"
          rules={[{ required: true, whitespace: true, message: "Enter a name" }]}
        >
          <Input maxLength={80} />
        </Form.Item>
        {canManage ? (
          <Button type="primary" htmlType="submit" loading={rename.isPending}>
            Save name
          </Button>
        ) : null}
      </Form>
      <Typography.Title level={5}>Proxy URL</Typography.Title>
      <ProxyUrl url={ws.proxyUrl} />
    </Card>
  );
}

function DangerZone({ ws }: { ws: Workspace }) {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const del = useDeleteWorkspace();
  return (
    <Card
      title="Danger zone"
      style={{ marginTop: token.marginLG, borderColor: token.colorErrorBorder }}
    >
      <Space vertical>
        <Typography.Text>
          Deleting a workspace removes its sessions and savings history, and its proxy URL stops
          working.
        </Typography.Text>
        {ws.isDefault ? (
          <Typography.Text type="secondary">
            The default workspace cannot be deleted.
          </Typography.Text>
        ) : null}
        <Popconfirm
          title={`Delete ${ws.name}?`}
          okText="Delete"
          okButtonProps={{ danger: true }}
          onConfirm={() =>
            del.mutate(ws.id, {
              onSuccess: () => message.success("Workspace deleted"),
              onError: () => message.error("Could not delete the workspace."),
            })
          }
        >
          <Button danger disabled={ws.isDefault} loading={del.isPending}>
            Delete workspace
          </Button>
        </Popconfirm>
      </Space>
    </Card>
  );
}

/** Settings > Workspace: name, proxy URL and the danger zone for the active workspace. */
export function WorkspacePage() {
  const me = useMe().data;
  const ws = useActiveWorkspace();
  const canManage = hasPermission(me, "workspaces.manage");
  return (
    <section aria-labelledby="page-title">
      <PageHead page={pageByKey("workspace")} />
      {ws ? (
        <>
          <General ws={ws} canManage={canManage} />
          {canManage ? <DangerZone ws={ws} /> : null}
        </>
      ) : (
        <Skeleton active />
      )}
    </section>
  );
}
