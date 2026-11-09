import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "antd";
import { describe, expect, it } from "vitest";
import type { Invitation } from "../../contract/generated";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { InvitationActions } from "./InvitationActions";

const INV: Invitation = {
  id: "inv_1",
  email: "new@acme.test",
  role: "member",
  status: "pending",
  invitedBy: "u1",
  expiresAt: "2026-12-01T00:00:00Z",
  createdAt: "2026-11-01T00:00:00Z",
};

const view = (inv: Invitation, isAdmin: boolean) => (
  <App>
    <InvitationActions invitation={inv} isAdmin={isAdmin} />
  </App>
);

describe("InvitationActions", () => {
  it("resends and revokes a member invitation", async () => {
    const api = mockApi({ invitations: [INV] });
    renderAt(view(INV, false));
    await userEvent.click(screen.getByRole("button", { name: "Resend" }));
    await waitFor(() =>
      expect(api.callsTo("POST", "/api/invitations/inv_1/resend")).toHaveLength(1),
    );
    await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
    // The Popconfirm's own Revoke button joins the row's after the first click.
    await waitFor(() => expect(screen.getAllByRole("button", { name: "Revoke" })).toHaveLength(2));
    await userEvent.click(screen.getAllByRole("button", { name: "Revoke" })[1] as HTMLElement);
    await waitFor(() => expect(api.callsTo("DELETE", "/api/invitations/inv_1")).toHaveLength(1));
  });

  it("locks both actions on an admin invitation unless the caller is an admin", () => {
    const admin: Invitation = { ...INV, role: "admin" };
    mockApi({ invitations: [admin] });
    const { unmount } = renderAt(view(admin, false));
    expect(screen.getByRole("button", { name: "Resend" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Revoke" })).toBeDisabled();
    unmount();
    renderAt(view(admin, true));
    expect(screen.getByRole("button", { name: "Resend" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Revoke" })).toBeEnabled();
  });
});
