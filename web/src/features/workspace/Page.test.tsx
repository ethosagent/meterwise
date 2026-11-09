import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { ADMIN_ME, MEMBER_ME, mockApi, TEST_WORKSPACES } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { WorkspacePage } from "./Page";

const PATH = "/settings/workspace";
const DEFAULT = TEST_WORKSPACES[0];
const STAGING = TEST_WORKSPACES[1];

describe("WorkspacePage", () => {
  it("shows the active workspace's name, proxy URL and Reset URL", async () => {
    mockApi();
    renderAt(<WorkspacePage />, PATH);
    expect(await screen.findByLabelText("Proxy URL")).toHaveValue(DEFAULT?.proxyUrl);
    expect(screen.getByLabelText("Name")).toHaveValue("Default workspace");
    expect(screen.getByRole("button", { name: "Reset URL" })).toBeInTheDocument();
  });

  it("without workspace_url:read the URL row says it is hidden and offers nothing", async () => {
    const noUrl = {
      admin: false,
      grants: MEMBER_SNAPSHOT.grants.filter((g) => g.resourceType !== "workspace_url"),
    };
    mockApi({ me: MEMBER_ME, access: { snapshot: noUrl } });
    renderAt(<WorkspacePage />, PATH);
    expect(await screen.findByText("The proxy URL is hidden for your account")).toBeInTheDocument();
    expect(screen.queryByLabelText("Proxy URL")).toBeNull();
    expect(screen.queryByRole("button", { name: "Reset URL" })).toBeNull();
    expect(screen.queryByText("Allowed client CIDRs")).toBeNull();
  });

  it("with workspace_url:read but not manage: the URL, no Reset URL, no CIDR editor", async () => {
    mockApi({ me: MEMBER_ME });
    renderAt(<WorkspacePage />, PATH);
    expect(await screen.findByLabelText("Proxy URL")).toHaveValue(DEFAULT?.proxyUrl);
    expect(screen.queryByRole("button", { name: "Reset URL" })).toBeNull();
    expect(screen.queryByText("Allowed client CIDRs")).toBeNull();
  });

  it("resets the URL from settings and the new URL replaces the old", async () => {
    const api = mockApi();
    renderAt(<WorkspacePage />, PATH);
    await screen.findByLabelText("Proxy URL");
    await userEvent.click(screen.getByRole("button", { name: "Reset URL" }));
    const pop = within(document.querySelector(".ant-popover") as HTMLElement);
    await userEvent.click(await pop.findByRole("button", { name: "Reset URL" }));
    await waitFor(() =>
      expect(api.callsTo("POST", "/api/workspaces/ws_1/url/reset")).toHaveLength(1),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("Proxy URL")).not.toHaveValue(DEFAULT?.proxyUrl),
    );
  });

  it("edits the allowed client CIDRs as tags, normalises a bare IP and saves with a PUT", async () => {
    const api = mockApi();
    renderAt(<WorkspacePage />, PATH);
    const input = await screen.findByLabelText("Allowed client CIDRs");
    expect(screen.getByRole("button", { name: "Save CIDRs" })).toBeDisabled();
    await userEvent.type(input, "10.0.0.0/8{Enter}");
    await userEvent.type(input, "203.0.113.7{Enter}");
    await userEvent.click(screen.getByRole("button", { name: "Save CIDRs" }));
    await waitFor(() =>
      expect(api.callsTo("PUT", "/api/workspaces/ws_1/url/allowed-cidrs")).toHaveLength(1),
    );
    expect(api.callsTo("PUT", "/api/workspaces/ws_1/url/allowed-cidrs")[0]?.body).toEqual({
      cidrs: ["10.0.0.0/8", "203.0.113.7"],
    });
    // the server normalised the bare IP; the saved list refetched from the workspace list
    expect(await screen.findByText("203.0.113.7/32")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save CIDRs" })).toBeDisabled();
  });

  it("shows the server's first problem when a CIDR is refused", async () => {
    mockApi();
    renderAt(<WorkspacePage />, PATH);
    const input = await screen.findByLabelText("Allowed client CIDRs");
    await userEvent.type(input, "not-an-ip{Enter}");
    await userEvent.click(screen.getByRole("button", { name: "Save CIDRs" }));
    expect(await screen.findByText("not-an-ip is not a valid IP or CIDR")).toBeInTheDocument();
  });

  it("copies the proxy URL", async () => {
    mockApi();
    const user = userEvent.setup();
    renderAt(<WorkspacePage />, PATH);
    await screen.findByLabelText("Proxy URL");
    await user.click(screen.getByRole("button", { name: /Copy/ }));
    expect(await navigator.clipboard.readText()).toBe(DEFAULT?.proxyUrl);
  });

  it("renames the workspace with a PATCH of the name only", async () => {
    const api = mockApi();
    renderAt(<WorkspacePage />, PATH);
    const name = await screen.findByLabelText("Name");
    await userEvent.clear(name);
    await userEvent.type(name, "Prod");
    await userEvent.click(screen.getByRole("button", { name: "Save name" }));
    await waitFor(() => expect(api.callsTo("PATCH", "/api/workspaces/ws_1")).toHaveLength(1));
    expect(api.callsTo("PATCH", "/api/workspaces/ws_1")[0]?.body).toEqual({ name: "Prod" });
  });

  it("does not let the default workspace be deleted", async () => {
    const api = mockApi();
    renderAt(<WorkspacePage />, PATH);
    const del = await screen.findByRole("button", { name: "Delete workspace" });
    expect(del).toBeDisabled();
    expect(screen.getByText("The default workspace cannot be deleted.")).toBeInTheDocument();
    await userEvent.click(del);
    expect(api.callsTo("DELETE", /.*/)).toHaveLength(0);
  });

  it("deletes a non-default workspace after confirmation and falls back to the default", async () => {
    const api = mockApi({ me: { ...ADMIN_ME, activeWorkspaceId: STAGING?.id ?? "" } });
    renderAt(<WorkspacePage />, PATH);
    await waitFor(() => expect(screen.getByLabelText("Name")).toHaveValue("Staging"));
    await userEvent.click(screen.getByRole("button", { name: "Delete workspace" }));
    const confirm = await screen.findByRole("button", { name: "Delete" });
    await userEvent.click(confirm);
    await waitFor(() => expect(api.callsTo("DELETE", "/api/workspaces/ws_2")).toHaveLength(1));
    await waitFor(() => expect(screen.getByLabelText("Name")).toHaveValue("Default workspace"));
  });

  it("gives a member the URL to copy but no admin controls", async () => {
    mockApi({ me: MEMBER_ME });
    renderAt(<WorkspacePage />, PATH);
    expect(await screen.findByLabelText("Proxy URL")).toHaveValue(DEFAULT?.proxyUrl);
    expect(screen.getByRole("button", { name: /Copy/ })).toBeEnabled();
    expect(screen.getByLabelText("Name")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Save name" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete workspace" })).toBeNull();
    expect(screen.queryByText("Danger zone")).toBeNull();
  });
});

describe("CreateWorkspaceDialog (from the switcher)", () => {
  async function openDialog() {
    const sw = await screen.findByTestId("workspace-switcher");
    await waitFor(() => expect(sw).toHaveTextContent("Default workspace"));
    await userEvent.click(sw);
    await userEvent.click(await screen.findByRole("menuitem", { name: "＋ New workspace" }));
    return within(await screen.findByRole("dialog"));
  }

  it("creates a workspace and shows its proxy URL with copy", async () => {
    const api = mockApi();
    const { Shell } = await import("../../shell/Shell");
    renderAt(<Shell />, "/overview");
    const dialog = await openDialog();
    await userEvent.type(dialog.getByLabelText("Name"), "Prod");
    await userEvent.click(dialog.getByRole("button", { name: "Create" }));
    const url = await dialog.findByLabelText("Proxy URL");
    expect((url as HTMLInputElement).value).toMatch(/^https:\/\/proxy\.acme\.test\/w\/pub/);
    expect(dialog.getByRole("button", { name: /Copy/ })).toBeInTheDocument();
    expect(api.callsTo("POST", "/api/workspaces")[0]?.body).toEqual({ name: "Prod" });
  });

  it("gives the dialog its own Name field even while the settings page has one", async () => {
    mockApi();
    const { AppRoutes } = await import("../../routes");
    renderAt(<AppRoutes />, "/settings/workspace");
    await screen.findByLabelText("Proxy URL");
    const dialog = await openDialog();
    const dialogName = dialog.getByLabelText("Name");
    expect(dialogName).toHaveValue("");
    expect(screen.getAllByLabelText("Name")).toHaveLength(2);
  });

  it("shows the limit error and creates nothing when the workspace limit is hit", async () => {
    mockApi({ maxWorkspaces: TEST_WORKSPACES.length });
    const { Shell } = await import("../../shell/Shell");
    renderAt(<Shell />, "/overview");
    const dialog = await openDialog();
    await userEvent.type(dialog.getByLabelText("Name"), "One too many");
    await userEvent.click(dialog.getByRole("button", { name: "Create" }));
    expect(await dialog.findByText(/Workspace limit reached/)).toBeInTheDocument();
    expect(dialog.queryByLabelText("Proxy URL")).toBeNull();
  });

  it("lets a member create a workspace (create is open to members)", async () => {
    const api = mockApi({ me: MEMBER_ME });
    const { Shell } = await import("../../shell/Shell");
    renderAt(<Shell />, "/overview");
    const dialog = await openDialog();
    await userEvent.type(dialog.getByLabelText("Name"), "Member made");
    await userEvent.click(dialog.getByRole("button", { name: "Create" }));
    await dialog.findByLabelText("Proxy URL");
    expect(api.callsTo("POST", "/api/workspaces")[0]?.body).toEqual({ name: "Member made" });
  });
});

describe("the active workspace is never substituted", () => {
  const GONE = {
    ...TEST_WORKSPACES[1],
    id: "ws_gone",
    name: "Gone",
  } as (typeof TEST_WORKSPACES)[number];

  it("shows an error, not the default workspace's name or URL, when the active id is not in the list", async () => {
    const api = mockApi({ me: { ...ADMIN_ME, activeWorkspaceId: "ws_gone" } });
    renderAt(<WorkspacePage />, PATH);
    expect(await screen.findByText("Your active workspace could not be found")).toBeInTheDocument();
    expect(screen.queryByLabelText("Proxy URL")).toBeNull();
    expect(screen.queryByDisplayValue("Default workspace")).toBeNull();
    expect(screen.queryByText("Delete workspace")).toBeNull();
    // it refetched both before giving up
    expect(api.callsTo("GET", "/api/workspaces").length).toBeGreaterThanOrEqual(2);
    expect(api.callsTo("GET", "/api/me").length).toBeGreaterThanOrEqual(2);
  });

  it("recovers when the refetch finds the workspace (a stale list)", async () => {
    mockApi({
      me: { ...ADMIN_ME, activeWorkspaceId: "ws_gone" },
      revealOnRefetch: [GONE],
    });
    renderAt(<WorkspacePage />, PATH);
    await waitFor(() => expect(screen.getByLabelText("Name")).toHaveValue("Gone"));
    expect(screen.getByLabelText("Proxy URL")).toHaveValue(GONE.proxyUrl);
  });

  it("labels the switcher as unavailable rather than naming the default", async () => {
    mockApi({ me: { ...ADMIN_ME, activeWorkspaceId: "ws_gone" } });
    const { Shell } = await import("../../shell/Shell");
    renderAt(<Shell />, "/overview");
    const sw = await screen.findByTestId("workspace-switcher");
    await waitFor(() => expect(sw).toHaveTextContent("Workspace unavailable"));
    expect(sw).not.toHaveTextContent("Default workspace");
  });
});
