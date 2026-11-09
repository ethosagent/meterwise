import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Invitation } from "../../api/identity-types";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { OrganizationPage } from "./Page";

const PATH = "/settings/organization";
const PENDING: Invitation = {
  id: "inv_1",
  email: "bo@acme.test",
  role: "member",
  status: "pending",
  invitedBy: "u1",
  expiresAt: "2026-12-01T00:00:00Z",
  createdAt: "2026-11-01T00:00:00Z",
};

describe("OrganizationPage (admin)", () => {
  it("shows the organization name and the invitations table", async () => {
    mockApi({ invitations: [PENDING] });
    renderAt(<OrganizationPage />, PATH);
    expect(await screen.findByText("bo@acme.test")).toBeInTheDocument();
    expect(screen.getAllByText("Acme Meter").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /Invite member/ })).toBeInTheDocument();
    expect(screen.queryByText(/^Members$/)).toBeNull();
  });

  it("invites with a chosen role", async () => {
    const api = mockApi();
    renderAt(<OrganizationPage />, PATH);
    await userEvent.click(await screen.findByRole("button", { name: /Invite member/ }));
    const dialog = within(await screen.findByRole("dialog"));
    await userEvent.type(dialog.getByLabelText("Email"), "cy@acme.test");
    await userEvent.click(dialog.getByRole("combobox"));
    await userEvent.click(await screen.findByTitle("Admin"));
    await userEvent.click(dialog.getByRole("button", { name: "Send invitation" }));
    await waitFor(() => expect(api.callsTo("POST", "/api/invitations")).toHaveLength(1));
    expect(api.callsTo("POST", "/api/invitations")[0]?.body).toEqual({
      email: "cy@acme.test",
      role: "admin",
    });
    expect(await screen.findByText("cy@acme.test")).toBeInTheDocument();
  });

  it("explains a duplicate invitation", async () => {
    mockApi({ invitations: [PENDING] });
    renderAt(<OrganizationPage />, PATH);
    await userEvent.click(await screen.findByRole("button", { name: /Invite member/ }));
    const dialog = within(await screen.findByRole("dialog"));
    await userEvent.type(dialog.getByLabelText("Email"), "bo@acme.test");
    await userEvent.click(dialog.getByRole("button", { name: "Send invitation" }));
    expect(await dialog.findByText(/already has a pending invitation/)).toBeInTheDocument();
  });

  it("resends and revokes a pending invitation", async () => {
    const api = mockApi({ invitations: [PENDING] });
    renderAt(<OrganizationPage />, PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Resend" }));
    await waitFor(() =>
      expect(api.callsTo("POST", "/api/invitations/inv_1/resend")).toHaveLength(1),
    );
    await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
    const popover = (await screen.findByText(/Revoke the invitation for bo@acme.test/)).closest(
      ".ant-popover",
    ) as HTMLElement;
    await userEvent.click(within(popover).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(api.callsTo("DELETE", "/api/invitations/inv_1")).toHaveLength(1));
    await waitFor(() => expect(screen.queryByText("bo@acme.test")).toBeNull());
  });
});

describe("OrganizationPage (member)", () => {
  it("shows the name only: no invitations table, no invite button, no invitations request", async () => {
    const api = mockApi({ me: MEMBER_ME, invitations: [PENDING] });
    renderAt(<OrganizationPage />, PATH);
    await waitFor(() => expect(api.callsTo("GET", "/api/me")).toHaveLength(1));
    await screen.findAllByText("Acme Meter");
    expect(screen.queryByRole("button", { name: /Invite member/ })).toBeNull();
    expect(screen.queryByText("bo@acme.test")).toBeNull();
    expect(screen.queryByText("Invitations")).toBeNull();
    expect(api.callsTo("GET", "/api/invitations")).toHaveLength(0);
  });
});
