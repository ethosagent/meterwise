import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ADMIN_ME, MEMBER_ME, mockApi, TEST_WORKSPACES } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { WorkspacePage } from "./Page";

const PATH = "/settings/workspace";
const DEFAULT = TEST_WORKSPACES[0];
const STAGING = TEST_WORKSPACES[1];

describe("WorkspacePage", () => {
  it("shows the active workspace's name and proxy URL, with no Reset URL", async () => {
    mockApi();
    renderAt(<WorkspacePage />, PATH);
    expect(await screen.findByLabelText("Proxy URL")).toHaveValue(DEFAULT?.proxyUrl);
    expect(screen.getByLabelText("Name")).toHaveValue("Default workspace");
    expect(screen.queryByText(/reset url/i)).toBeNull();
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

  it("offers no create entry to a member", async () => {
    mockApi({ me: MEMBER_ME });
    const { Shell } = await import("../../shell/Shell");
    renderAt(<Shell />, "/overview");
    const sw = await screen.findByTestId("workspace-switcher");
    await waitFor(() => expect(sw).toHaveTextContent("Default workspace"));
    await userEvent.click(sw);
    await screen.findByRole("menuitem", { name: "Staging" });
    expect(screen.queryByRole("menuitem", { name: "＋ New workspace" })).toBeNull();
  });
});
