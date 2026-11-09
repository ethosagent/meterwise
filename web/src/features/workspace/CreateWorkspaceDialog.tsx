import { Alert, Button, Form, Input, Modal, Typography, theme } from "antd";
import { useState } from "react";
import type { WorkspaceView } from "../../api/contract04";
import { describeApiError } from "../../api/errors";
import { IDENTITY_ERRORS, useCreateWorkspace } from "../../api/identity";
import { ProxyUrl } from "./ProxyUrl";
import { UrlHidden } from "./WorkspaceUrl";

/** What to tell the person when a create fails; the limit is the one they can act on. */
export function createErrorText(e: unknown): string {
  return describeApiError(e, {
    byCode: {
      [IDENTITY_ERRORS.workspaceLimit]:
        "Workspace limit reached. Delete a workspace you no longer use, or ask the operator to raise the limit.",
    },
    fallback: "Could not create the workspace. Try again.",
  });
}

/** Name in, proxy URL out: creating a workspace is the whole setup. */
export function CreateWorkspaceDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { token } = theme.useToken();
  const create = useCreateWorkspace();
  const [created, setCreated] = useState<WorkspaceView | null>(null);
  const [form] = Form.useForm<{ name: string }>();

  const close = () => {
    setCreated(null);
    create.reset();
    form.resetFields();
    onClose();
  };

  return (
    <Modal
      open={open}
      title={created ? "Workspace created" : "New workspace"}
      onCancel={close}
      destroyOnHidden
      footer={
        created ? (
          <Button type="primary" onClick={close}>
            Done
          </Button>
        ) : (
          <>
            <Button onClick={close}>Cancel</Button>
            <Button type="primary" loading={create.isPending} onClick={() => form.submit()}>
              Create
            </Button>
          </>
        )
      }
    >
      {created ? (
        <>
          <Typography.Paragraph>
            Point your agent&apos;s base URL at this address. Keep your own provider keys.
          </Typography.Paragraph>
          {created.proxyUrl === null ? <UrlHidden /> : <ProxyUrl url={created.proxyUrl} />}
        </>
      ) : (
        <Form
          name="createWorkspace"
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(v) => create.mutate(v.name.trim(), { onSuccess: (w) => setCreated(w) })}
        >
          {create.isError ? (
            <Alert
              type="error"
              showIcon
              title={createErrorText(create.error)}
              style={{ marginBottom: token.marginMD }}
            />
          ) : null}
          <Form.Item
            label="Name"
            name="name"
            rules={[{ required: true, whitespace: true, message: "Enter a name" }]}
          >
            <Input autoFocus maxLength={80} />
          </Form.Item>
        </Form>
      )}
    </Modal>
  );
}
