import { Alert, Button, Form, Input, Modal, Select } from "antd";
import { ACCESS_ERROR_TEXT } from "../../api/access";
import { describeApiError } from "../../api/errors";
import { IDENTITY_ERRORS, useInvite } from "../../api/identity";
import { useIsAdmin } from "../../auth/usePermission";
import type { Role } from "../../contract/generated";

function inviteErrorText(e: unknown): string {
  return describeApiError(e, {
    byCode: {
      ...ACCESS_ERROR_TEXT,
      [IDENTITY_ERRORS.duplicateInvitation]:
        "That address already has a pending invitation. Resend or revoke it below.",
    },
    fallback: "Could not send the invitation. Try again.",
  });
}

/** Email and role in; the server creates the invitation and emails the link. */
export function InviteModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  // The escalation guard: only an admin may invite another admin (403 "admin required" otherwise).
  const canGrantAdmin = useIsAdmin();
  const roles: { value: Role; label: string; disabled?: boolean }[] = [
    { value: "member", label: "Member" },
    { value: "admin", label: "Admin", disabled: !canGrantAdmin },
  ];
  const invite = useInvite();
  const [form] = Form.useForm<{ email: string; role: Role }>();
  const close = () => {
    invite.reset();
    form.resetFields();
    onClose();
  };
  return (
    <Modal
      open={open}
      title="Invite member"
      onCancel={close}
      destroyOnHidden
      footer={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="primary" loading={invite.isPending} onClick={() => form.submit()}>
            Send invitation
          </Button>
        </>
      }
    >
      <Form
        name="invite"
        form={form}
        layout="vertical"
        requiredMark={false}
        initialValues={{ role: "member" }}
        onFinish={(v) =>
          invite.mutate({ email: v.email.trim(), role: v.role }, { onSuccess: close })
        }
      >
        {invite.isError ? (
          <Alert type="error" showIcon title={inviteErrorText(invite.error)} />
        ) : null}
        <Form.Item
          label="Email"
          name="email"
          rules={[{ required: true, type: "email", message: "Enter a valid email" }]}
        >
          <Input autoComplete="off" />
        </Form.Item>
        <Form.Item label="Role" name="role">
          <Select options={roles} aria-label="Role" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
