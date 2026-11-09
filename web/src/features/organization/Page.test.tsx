import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Invitation } from "../../contract/generated";
import { ADA, BO } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { OrganizationPage } from "./Page";

const PATH = "/settings/organization";
const PENDING: Invitation = {
  id: "inv_1",
  email: "cy@acme.test",
  role: "member",
  status: "pending",
  invitedBy: "u1",
  expiresAt: "2026-12-01T00:00:00Z",
  createdAt: "2026-11-01T00:00:00Z",
};
const OPTION = { selector: ".ant-select-item-option-content" };

async function inviteDialog() {
  await userEvent.click(await screen.findByRole("button", { name: /Invite member/ }));
  return within(await screen.findByRole("dialog"));
}

async function confirmIn(title: RegExp, button: string) {
  const popover = (await screen.findByText(title)).closest(".ant-popover") as HTMLElement;
  await userEvent.click(within(popover).getByRole("button", { name: button }));
}

describe("OrganizationPage (admin)", () => {
  it("lists members and the pending invitation with a pending tag", async () => {
    mockApi({ invitations: [PENDING] });
    renderAt(<OrganizationPage />, PATH);
    expect(await screen.findByText("cy@acme.test")).toBeInTheDocument();
    expect(screen.getByText("Ada Admin")).toBeInTheDocument();
    expect(screen.getByText("Bo Member")).toBeInTheDocument();
    expect(screen.getByText("Pending invite")).toBeInTheDocument();
    expect(screen.getAllByText("Acme Meter").length).toBeGreaterThan(0);
  });

  it("invites with a chosen role", async () => {
    const api = mockApi();
    renderAt(<OrganizationPage />, PATH);
    const dialog = await inviteDialog();
    await userEvent.type(dialog.getByLabelText("Email"), "di@acme.test");
    await userEvent.click(dialog.getByRole("combobox", { name: "Role" }));
    await userEvent.click(await screen.findByText("Admin", OPTION));
    await userEvent.click(dialog.getByRole("button", { name: "Send invitation" }));
    await waitFor(() => expect(api.callsTo("POST", "/api/invitations")).toHaveLength(1));
    expect(api.callsTo("POST", "/api/invitations")[0]?.body).toEqual({
      email: "di@acme.test",
      role: "admin",
    });
    expect(await screen.findByText("di@acme.test")).toBeInTheDocument();
  });

  it("explains a duplicate invitation", async () => {
    mockApi({ invitations: [PENDING] });
    renderAt(<OrganizationPage />, PATH);
    const dialog = await inviteDialog();
    await userEvent.type(dialog.getByLabelText("Email"), "cy@acme.test");
    await userEvent.click(dialog.getByRole("button", { name: "Send invitation" }));
    expect(await dialog.findByText(/already has a pending invitation/)).toBeInTheDocument();
  });

  it("says so when the person is already a member (409 already_member)", async () => {
    mockApi();
    renderAt(<OrganizationPage />, PATH);
    const dialog = await inviteDialog();
    await userEvent.type(dialog.getByLabelText("Email"), BO.email);
    await userEvent.click(dialog.getByRole("button", { name: "Send invitation" }));
    expect(await dialog.findByText(/already a member of the organization/)).toBeInTheDocument();
  });

  it("resends and revokes a pending invitation", async () => {
    const api = mockApi({ invitations: [PENDING] });
    renderAt(<OrganizationPage />, PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Resend" }));
    await waitFor(() =>
      expect(api.callsTo("POST", "/api/invitations/inv_1/resend")).toHaveLength(1),
    );
    await userEvent.click(screen.getByRole("button", { name: "Revoke" }));
    await confirmIn(/Revoke the invitation for cy@acme.test/, "Revoke");
    await waitFor(() => expect(api.callsTo("DELETE", "/api/invitations/inv_1")).toHaveLength(1));
    await waitFor(() => expect(screen.queryByText("cy@acme.test")).toBeNull());
  });

  it("changes a role with PATCH and removes a member after confirmation", async () => {
    const api = mockApi();
    renderAt(<OrganizationPage />, PATH);
    await userEvent.click(await screen.findByRole("combobox", { name: `Role for ${BO.email}` }));
    await userEvent.click(await screen.findByText("Admin", OPTION));
    await waitFor(() => expect(api.callsTo("PATCH", `/api/members/${BO.userId}`)).toHaveLength(1));
    expect(api.callsTo("PATCH", `/api/members/${BO.userId}`)[0]?.body).toEqual({ role: "admin" });

    const row = screen.getByText("Bo Member").closest("tr") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Remove" }));
    await confirmIn(/Remove bo@acme.test from the organization/, "Remove");
    await waitFor(() => expect(api.callsTo("DELETE", `/api/members/${BO.userId}`)).toHaveLength(1));
  });

  it("explains last_admin when the only admin is demoted", async () => {
    mockApi();
    renderAt(<OrganizationPage />, PATH);
    await userEvent.click(await screen.findByRole("combobox", { name: `Role for ${ADA.email}` }));
    await userEvent.click(await screen.findByText("Member", OPTION));
    expect(await screen.findByText(/without an admin/)).toBeInTheDocument();
  });

  it("explains last_admin when the only admin is removed", async () => {
    mockApi();
    renderAt(<OrganizationPage />, PATH);
    const row = (await screen.findByText("Ada Admin")).closest("tr") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Remove" }));
    await confirmIn(/Remove ada@acme.test from the organization/, "Remove");
    expect(await screen.findByText(/without an admin/)).toBeInTheDocument();
  });
});

const READ = { workspaceId: null, resourceType: "member", action: "read" } as const;
const MANAGE = { ...READ, action: "manage" } as const;

describe("OrganizationPage (member)", () => {
  it("shows the name only without member:read: no table, no invite, no members or invitations request", async () => {
    const api = mockApi({ me: MEMBER_ME, invitations: [PENDING] });
    renderAt(<OrganizationPage />, PATH);
    await waitFor(() => expect(api.callsTo("GET", "/api/me/permissions")).toHaveLength(1));
    await screen.findAllByText("Acme Meter");
    expect(screen.queryByRole("button", { name: /Invite member/ })).toBeNull();
    expect(screen.queryByText("Members")).toBeNull();
    expect(api.callsTo("GET", "/api/invitations")).toHaveLength(0);
    expect(api.callsTo("GET", "/api/members")).toHaveLength(0);
  });

  it("with member:read only: sees members, but no invite, role Select, Remove or pending rows", async () => {
    const api = mockApi({
      me: MEMBER_ME,
      invitations: [PENDING],
      access: { snapshot: { admin: false, grants: [READ] } },
    });
    renderAt(<OrganizationPage />, PATH);
    expect(await screen.findByText("Bo Member")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Invite member/ })).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    expect(screen.queryByText("Pending invite")).toBeNull();
    expect(api.callsTo("GET", "/api/invitations")).toHaveLength(0);
  });

  it("with member:manage: invites and resends, but cannot invite an admin or change roles", async () => {
    mockApi({
      me: MEMBER_ME,
      invitations: [PENDING],
      access: { snapshot: { admin: false, grants: [READ, MANAGE] } },
    });
    renderAt(<OrganizationPage />, PATH);
    expect(await screen.findByRole("button", { name: "Resend" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: /Role for/ })).toBeNull();
    const dialog = await inviteDialog();
    await userEvent.click(dialog.getByRole("combobox", { name: "Role" }));
    const admin = await screen.findByText("Admin", OPTION);
    expect(admin.closest(".ant-select-item")).toHaveAttribute("aria-disabled", "true");
  });
});
