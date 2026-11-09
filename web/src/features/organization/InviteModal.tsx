import { Alert, Button, Form, Input, Modal, Select } from "antd";
import { ApiError } from "../../api/errors";
import { useInvite } from "../../api/identity";
import { IDENTITY_ERRORS, type Role } from "../../api/identity-types";

const ROLES: { value: Role; label: string }[] = [
  { value: "member", label: "Member" },
  { value: "admin", label: "Admin" },
];

function inviteErrorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === IDENTITY_ERRORS.duplicateInvitation) {
      return "That address already has a pending invitation. Resend or revoke it below.";
    }
    if (e.status === 422) return e.problems[0] ?? e.message;
  }
  return "Could not send the invitation. Try again.";
}

/** Email and role in; the server creates the invitation and emails the link. */
export function InviteModal({ open, onClose }: { open: boolean; onClose: () => void }) {
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
        form={form}
        layout="vertical"
        requiredMark={false}
        initialValues={{ role: "member" }}
        onFinish={(v) =>
          invite.mutate({ email: v.email.trim(), role: v.role }, { onSuccess: close })
        }
      >
        {invite.isError ? (
          <Alert type="error" showIcon message={inviteErrorText(invite.error)} />
        ) : null}
        <Form.Item
          label="Email"
          name="email"
          rules={[{ required: true, type: "email", message: "Enter a valid email" }]}
        >
          <Input autoComplete="off" />
        </Form.Item>
        <Form.Item label="Role" name="role">
          <Select options={ROLES} aria-label="Role" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
