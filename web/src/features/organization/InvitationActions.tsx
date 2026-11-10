import { App, Button, Popconfirm } from "antd";
import { ACCESS_ERROR_TEXT } from "../../api/access";
import { useResendInvitation, useRevokeInvitation } from "../../api/identity";
import { useErrorToast } from "../../api/useErrorToast";
import type { Invitation } from "../../contract/generated";

/**
 * Resend and Revoke for one pending invitation. Only an admin may touch an admin invitation (the
 * escalation guard answers 403 otherwise), so both are disabled for everyone else.
 */
export function InvitationActions({
  invitation,
  isAdmin,
}: {
  invitation: Invitation;
  isAdmin: boolean;
}) {
  const { message } = App.useApp();
  const fail = useErrorToast(ACCESS_ERROR_TEXT);
  const resend = useResendInvitation();
  const revoke = useRevokeInvitation();
  const locked = invitation.role === "admin" && !isAdmin;
  return (
    <>
      <Button
        type="link"
        disabled={locked}
        loading={resend.isPending && resend.variables === invitation.id}
        onClick={() =>
          resend.mutate(invitation.id, {
            onSuccess: () => message.success(`Invitation resent to ${invitation.email}`),
            onError: (e) => fail(e, "Could not resend the invitation."),
          })
        }
      >
        Resend
      </Button>
      <Popconfirm
        title={`Revoke the invitation for ${invitation.email}?`}
        okText="Revoke"
        okButtonProps={{ danger: true }}
        onConfirm={() =>
          revoke.mutate(invitation.id, {
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
