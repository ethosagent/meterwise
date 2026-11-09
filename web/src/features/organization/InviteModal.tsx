import { Form, Input, Select } from "antd";
import { ACCESS_ERROR_TEXT } from "../../api/access";
import { describeApiError } from "../../api/errors";
import { IDENTITY_ERRORS, useInvite } from "../../api/identity";
import { useIsAdmin } from "../../auth/usePermission";
import type { Role } from "../../contract/generated";
import { MutationFormModal } from "../../shell/MutationFormModal";

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
  return (
    <MutationFormModal<{ email: string; role: Role }>
      open={open}
      onClose={onClose}
      name="invite"
      title="Invite member"
      submitText="Send invitation"
      mutation={invite}
      errorText={inviteErrorText}
      initialValues={{ role: "member" }}
      onSubmit={(v, done) =>
        invite.mutate({ email: v.email.trim(), role: v.role }, { onSuccess: done })
      }
    >
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
    </MutationFormModal>
  );
}
