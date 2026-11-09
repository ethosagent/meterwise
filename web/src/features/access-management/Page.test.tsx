import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { PermissionSnapshot } from "../../access/plan03-types";
import { BO, type MockGroup } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { AccessManagementPage } from "./Page";

const PATH = "/settings/access";
const ENGINEERS: MockGroup = {
  id: "g1",
  name: "Engineers",
  memberCount: 0,
  workspaceIds: ["ws_1"],
  createdAt: "2026-11-01T00:00:00Z",
  grants: [{ workspaceId: "ws_1", resourceType: "session", action: "read" }],
  memberIds: [],
};
const MEMBER_READ: PermissionSnapshot = {
  admin: false,
  grants: [
    { workspaceId: null, resourceType: "member", action: "read" },
    { workspaceId: null, resourceType: "workspace", action: "read" },
  ],
};
const OPTION = { selector: ".ant-select-item-option-content" };

async function openDrawer() {
  await userEvent.click(await screen.findByRole("button", { name: "Engineers" }));
  return within(await screen.findByRole("dialog"));
}

describe("AccessManagementPage (admin)", () => {
  it("shows the Groups, Users and Invitations tabs", async () => {
    mockApi({ access: { groups: [ENGINEERS] } });
    renderAt(<AccessManagementPage />, PATH);
    for (const name of ["Groups", "Users", "Invitations"]) {
      expect(await screen.findByRole("tab", { name })).toBeInTheDocument();
    }
    expect(await screen.findByText("Engineers")).toBeInTheDocument();
  });

  it("lists users with their groups", async () => {
    mockApi({
      access: {
        groups: [{ ...ENGINEERS, memberIds: [BO.userId] }],
        members: [{ ...BO, groupIds: ["g1"] }],
      },
    });
    renderAt(<AccessManagementPage />, PATH);
    await userEvent.click(await screen.findByRole("tab", { name: "Users" }));
    const row = (await screen.findByText("bo@acme.test")).closest("tr") as HTMLElement;
    expect(await within(row).findByText("Engineers")).toBeInTheDocument();
  });

  it("creates a group, and explains group_name_taken", async () => {
    const api = mockApi({ access: { groups: [ENGINEERS] } });
    renderAt(<AccessManagementPage />, PATH);
    await userEvent.click(await screen.findByRole("button", { name: /New group/ }));
    const dialog = within(await screen.findByRole("dialog"));
    await userEvent.type(dialog.getByLabelText("Name"), "engineers");
    await userEvent.click(dialog.getByRole("button", { name: "Create group" }));
    expect(await dialog.findByText(/group with that name already exists/)).toBeInTheDocument();
    await userEvent.clear(dialog.getByLabelText("Name"));
    await userEvent.type(dialog.getByLabelText("Name"), "Analysts");
    await userEvent.click(dialog.getByRole("button", { name: "Create group" }));
    await waitFor(() => expect(api.callsTo("POST", "/api/groups")).toHaveLength(2));
    expect(await screen.findByText("Analysts")).toBeInTheDocument();
  });

  it("edits the permissions matrix and saves all grants in one PUT", async () => {
    const api = mockApi({ access: { groups: [ENGINEERS] } });
    renderAt(<AccessManagementPage />, PATH);
    const drawer = await openDrawer();
    const save = await drawer.findByRole("button", { name: "Save permissions" });
    expect(save).toBeDisabled();
    expect(drawer.getByLabelText("Default workspace: session:read")).toBeChecked();
    await userEvent.click(drawer.getByLabelText("Default workspace: workspace:write"));
    await userEvent.click(drawer.getByLabelText("Organization: member:read"));
    await userEvent.click(save);
    await waitFor(() => expect(api.callsTo("PUT", "/api/groups/g1/permissions")).toHaveLength(1));
    expect(api.callsTo("PUT", "/api/groups/g1/permissions")[0]?.body).toEqual({
      grants: [
        { workspaceId: "ws_1", resourceType: "session", action: "read" },
        { workspaceId: "ws_1", resourceType: "workspace", action: "write" },
        { workspaceId: null, resourceType: "member", action: "read" },
      ],
    });
    // the snapshot is refetched after a grant change
    await waitFor(() =>
      expect(api.callsTo("GET", "/api/me/permissions").length).toBeGreaterThan(1),
    );
  });

  it("shows only valid actions per resource (session has read only)", async () => {
    mockApi({ access: { groups: [ENGINEERS] } });
    renderAt(<AccessManagementPage />, PATH);
    const drawer = await openDrawer();
    await drawer.findByLabelText("Default workspace: session:read");
    expect(drawer.queryByLabelText("Default workspace: session:write")).toBeNull();
    expect(drawer.queryByLabelText("Default workspace: session:manage")).toBeNull();
  });

  it("attaches and detaches a workspace", async () => {
    const api = mockApi({ access: { groups: [ENGINEERS] } });
    renderAt(<AccessManagementPage />, PATH);
    const drawer = await openDrawer();
    await userEvent.click(await drawer.findByRole("combobox", { name: "Attach a workspace" }));
    await userEvent.click(await screen.findByText("Staging", OPTION));
    await waitFor(() => expect(api.callsTo("POST", "/api/groups/g1/workspaces")).toHaveLength(1));
    expect(api.callsTo("POST", "/api/groups/g1/workspaces")[0]?.body).toEqual({
      workspaceId: "ws_2",
    });
    expect(await drawer.findByLabelText("Staging: workspace:read")).toBeChecked();
    const tag = drawer.getAllByText("Staging").find((e) => e.closest(".ant-tag"));
    const close = tag?.closest(".ant-tag")?.querySelector(".ant-tag-close-icon");
    await userEvent.click(close as Element);
    await waitFor(() =>
      expect(api.callsTo("DELETE", "/api/groups/g1/workspaces/ws_2")).toHaveLength(1),
    );
    await waitFor(() => expect(drawer.queryByLabelText("Staging: workspace:read")).toBeNull());
  });

  it("moves a user into and out of the group with the transfer list (group_users only)", async () => {
    const api = mockApi({ access: { groups: [ENGINEERS] } });
    renderAt(<AccessManagementPage />, PATH);
    const drawer = await openDrawer();
    await userEvent.click(await drawer.findByRole("tab", { name: "Members" }));
    await userEvent.click(await drawer.findByText("Bo Member (bo@acme.test)"));
    await userEvent.click(drawer.getByRole("button", { name: "right" }));
    await waitFor(() => expect(api.callsTo("POST", "/api/groups/g1/members")).toHaveLength(1));
    expect(api.callsTo("POST", "/api/groups/g1/members")[0]?.body).toEqual({ userId: BO.userId });
    expect(api.callsTo("PUT", /\/api\/groups\/g1\/permissions/)).toHaveLength(0);
    await userEvent.click(await drawer.findByText("Bo Member (bo@acme.test)"));
    await userEvent.click(drawer.getByRole("button", { name: "left" }));
    await waitFor(() =>
      expect(api.callsTo("DELETE", `/api/groups/g1/members/${BO.userId}`)).toHaveLength(1),
    );
  });

  it("renames and deletes a group", async () => {
    const api = mockApi({ access: { groups: [ENGINEERS] } });
    renderAt(<AccessManagementPage />, PATH);
    const drawer = await openDrawer();
    const name = drawer.getByLabelText("Group name");
    await userEvent.clear(name);
    await userEvent.type(name, "Builders");
    await userEvent.click(drawer.getByRole("button", { name: "Rename" }));
    await waitFor(() => expect(api.callsTo("PATCH", "/api/groups/g1")).toHaveLength(1));
    expect(api.callsTo("PATCH", "/api/groups/g1")[0]?.body).toEqual({ name: "Builders" });
    await userEvent.click(drawer.getByRole("button", { name: "Delete group" }));
    const popover = (await screen.findByText(/^Delete .*\?$/)).closest(
      ".ant-popover",
    ) as HTMLElement;
    await userEvent.click(within(popover).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(api.callsTo("DELETE", "/api/groups/g1")).toHaveLength(1));
  });
});

describe("AccessManagementPage (member)", () => {
  it("with member:read: reads groups, every write control is hidden or disabled, no Invitations tab", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { groups: [ENGINEERS], snapshot: MEMBER_READ } });
    renderAt(<AccessManagementPage />, PATH);
    expect(await screen.findByRole("tab", { name: "Groups" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Invitations" })).toBeNull();
    expect(screen.queryByRole("button", { name: /New group/ })).toBeNull();
    const drawer = await openDrawer();
    expect(await drawer.findByLabelText("Default workspace: session:read")).toBeDisabled();
    expect(drawer.getByRole("button", { name: "Save permissions" })).toBeDisabled();
    expect(drawer.getByLabelText("Group name")).toBeDisabled();
    expect(drawer.getByRole("combobox", { name: "Attach a workspace" })).toBeDisabled();
    expect(drawer.queryByRole("button", { name: "Delete group" })).toBeNull();
    expect(api.callsTo("GET", "/api/groups").length).toBeGreaterThan(0);
  });

  it("without member:read or member:manage: says the page is off limits and calls no access endpoint", async () => {
    const api = mockApi({ me: MEMBER_ME });
    renderAt(<AccessManagementPage />, PATH);
    expect(await screen.findByText("You do not have access to this page")).toBeInTheDocument();
    expect(screen.queryByRole("tab")).toBeNull();
    expect(api.callsTo("GET", "/api/groups")).toHaveLength(0);
    expect(api.callsTo("GET", "/api/members")).toHaveLength(0);
  });
});

describe("Organization defaults tab", () => {
  const open = async () => {
    await userEvent.click(await screen.findByRole("tab", { name: "Defaults" }));
    return screen.findByText("Organization defaults apply to all members in all workspaces");
  };

  it("tells admins the defaults reach everyone everywhere, and saves the whole set in one PUT", async () => {
    const api = mockApi();
    renderAt(<AccessManagementPage />, PATH);
    await open();
    const sessionRead = await screen.findByLabelText("Every workspace: session:read");
    expect(sessionRead).toBeChecked(); // the seed
    await userEvent.click(sessionRead);
    await userEvent.click(screen.getByLabelText("Organization: member:read"));
    await userEvent.click(screen.getByRole("button", { name: "Save permissions" }));
    await waitFor(() =>
      expect(api.callsTo("PUT", "/api/organization/permissions")).toHaveLength(1),
    );
    const sent = api.callsTo("PUT", "/api/organization/permissions")[0]?.body as
      | { grants: unknown[] }
      | undefined;
    const grants = sent?.grants ?? [];
    expect(grants).toHaveLength(5); // 5 seeded reads, minus session, plus member:read
    expect(grants).toContainEqual({ workspaceId: null, resourceType: "member", action: "read" });
    expect(grants).not.toContainEqual({
      workspaceId: null,
      resourceType: "session",
      action: "read",
    });
    expect(grants.every((g) => (g as { workspaceId: unknown }).workspaceId === null)).toBe(true);
    await waitFor(() =>
      expect(api.callsTo("GET", "/api/me/permissions").length).toBeGreaterThan(1),
    );
  });

  it("is read-only for a member with member:read", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: MEMBER_READ } });
    renderAt(<AccessManagementPage />, PATH);
    await open();
    expect(await screen.findByLabelText("Every workspace: session:read")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save permissions" })).toBeDisabled();
    expect(api.callsTo("PUT", /organization/)).toHaveLength(0);
  });

  it("has no Defaults tab without member:read", async () => {
    mockApi({
      me: MEMBER_ME,
      access: {
        snapshot: {
          admin: false,
          grants: [{ workspaceId: null, resourceType: "member", action: "manage" }],
        },
      },
    });
    renderAt(<AccessManagementPage />, PATH);
    expect(await screen.findByRole("tab", { name: "Invitations" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Defaults" })).toBeNull();
  });
});
