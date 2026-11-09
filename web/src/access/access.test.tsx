import { QueryObserver } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { permissionsKey } from "../api/access";
import { ApiError } from "../api/errors";
import { meKey, workspacesKey } from "../api/identity";
import { createQueryClient } from "../api/queryClient";
import type { Grant, PermissionSnapshot } from "../contract/generated";
import { AppRoutes } from "../routes";
import { MEMBER_ME, mockApi } from "../test/mockApi";
import { renderAt } from "../test/render";

const READS: Grant[] = (["workspace", "session", "savings"] as const).map((resourceType) => ({
  workspaceId: null,
  resourceType,
  action: "read",
}));
const WRITE_WS1: Grant = { workspaceId: "ws_1", resourceType: "workspace", action: "write" };
const ONE_WORKSPACE: PermissionSnapshot = { admin: false, grants: [...READS, WRITE_WS1] };

describe("grants decide what a control does", () => {
  it("member with a grant in one workspace: the name field is enabled there and disabled in another", async () => {
    mockApi({ me: MEMBER_ME, access: { snapshot: ONE_WORKSPACE } });
    renderAt(<AppRoutes />, "/settings/workspace");
    expect(await screen.findByLabelText("Name")).toHaveValue("Default workspace");
    await waitFor(() => expect(screen.getByLabelText("Name")).toBeEnabled());
    expect(screen.getByRole("button", { name: "Save name" })).toBeInTheDocument();

    await userEvent.click(screen.getByTestId("workspace-switcher"));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Staging" }));
    await waitFor(() => expect(screen.getByLabelText("Name")).toHaveValue("Staging"));
    expect(screen.getByLabelText("Name")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Save name" })).toBeNull();
  });

  it("admin bypass: an admin with zero grant rows can rename and delete", async () => {
    const api = mockApi({ access: { snapshot: { admin: true, grants: [] } } });
    renderAt(<AppRoutes />, "/settings/workspace");
    expect(await screen.findByRole("button", { name: "Save name" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete workspace" })).toBeInTheDocument();
    expect(api.access.snapshot.grants).toHaveLength(0);
  });

  it("an org default (null workspace) applies in every workspace", async () => {
    const orgWide: Grant = { ...WRITE_WS1, workspaceId: null };
    mockApi({ me: MEMBER_ME, access: { snapshot: { admin: false, grants: [...READS, orgWide] } } });
    renderAt(<AppRoutes />, "/settings/workspace");
    await waitFor(() => expect(screen.getByLabelText("Name")).toBeEnabled());
    await userEvent.click(screen.getByTestId("workspace-switcher"));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Research" }));
    await waitFor(() => expect(screen.getByLabelText("Name")).toHaveValue("Research"));
    expect(screen.getByLabelText("Name")).toBeEnabled();
  });

  it("hides Access management from the sidebar without member:read", async () => {
    mockApi({ me: MEMBER_ME });
    renderAt(<AppRoutes />, "/settings/profile");
    const nav = await screen.findByRole("menu", { name: "Main navigation" });
    await waitFor(() => expect(nav).toHaveTextContent("Organization"));
    expect(nav).not.toHaveTextContent("Access management");
  });
});

describe("a 403 means the snapshot was stale", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("refetches the snapshot, the workspace list and Me, and the control goes away", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: ONE_WORKSPACE } });
    renderAt(<AppRoutes />, "/settings/workspace");
    const name = await screen.findByLabelText("Name");
    await waitFor(() => expect(name).toBeEnabled());
    const before = {
      snap: api.callsTo("GET", "/api/me/permissions").length,
      list: api.callsTo("GET", "/api/workspaces").length,
      me: api.callsTo("GET", "/api/me").length,
    };

    api.access.setSnapshot({ admin: false, grants: READS }); // revoked elsewhere; the UI does not know yet
    await userEvent.clear(name);
    await userEvent.type(name, "Prod");
    await userEvent.click(screen.getByRole("button", { name: "Save name" }));

    await waitFor(() => expect(api.callsTo("PATCH", "/api/workspaces/ws_1")).toHaveLength(1));
    await waitFor(() =>
      expect(api.callsTo("GET", "/api/me/permissions").length).toBeGreaterThan(before.snap),
    );
    expect(api.callsTo("GET", "/api/workspaces").length).toBeGreaterThan(before.list);
    expect(api.callsTo("GET", "/api/me").length).toBeGreaterThan(before.me);
    await waitFor(() => expect(screen.queryByRole("button", { name: "Save name" })).toBeNull());
  });

  it("does not loop when the snapshot request itself is refused", async () => {
    const client = createQueryClient(false);
    const spy = vi.spyOn(client, "invalidateQueries");
    const refuse = () =>
      Promise.reject(new ApiError(403, { code: "forbidden", message: "no", details: {} }));
    await client.fetchQuery({ queryKey: permissionsKey, queryFn: refuse }).catch(() => undefined);
    expect(spy).not.toHaveBeenCalled();
    await client.fetchQuery({ queryKey: ["other"], queryFn: refuse }).catch(() => undefined);
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(3));
  });

  it.each([
    ["me", meKey],
    ["workspaces", workspacesKey],
  ])("a persistent 403 on %s refreshes access once and then stops", async (_n, key) => {
    vi.useFakeTimers();
    const client = createQueryClient(false);
    let refused = 0;
    let snapshots = 0;
    const watch = (queryKey: readonly unknown[], queryFn: () => Promise<unknown>) =>
      new QueryObserver(client, { queryKey, queryFn }).subscribe(() => undefined);
    const stops = [
      watch(key, () => {
        refused++;
        return Promise.reject(new ApiError(403, { code: "forbidden", message: "no", details: {} }));
      }),
      watch(permissionsKey, async () => {
        snapshots++;
        return { admin: false, grants: [] };
      }),
    ];
    await vi.advanceTimersByTimeAsync(300); // settle: a loop would keep counting
    expect(refused).toBeLessThanOrEqual(3);
    expect(snapshots).toBeLessThanOrEqual(3);
    const settled = refused;
    await vi.advanceTimersByTimeAsync(200);
    expect(refused).toBe(settled);
    for (const stop of stops) stop();
    vi.useRealTimers();
  });
});

describe("no workspace access", () => {
  const NONE = { ...MEMBER_ME, activeWorkspaceId: null };

  it("shows the empty state on a workspace page, and the switcher says so", async () => {
    mockApi({ me: NONE, access: { snapshot: { admin: false, grants: [] } } });
    renderAt(<AppRoutes />, "/overview");
    expect(await screen.findByText("You do not have access to any workspace")).toBeInTheDocument();
    expect(screen.getByText(/Ask an organization admin/)).toBeInTheDocument();
    expect(screen.getByTestId("workspace-switcher")).toHaveTextContent("No workspace access");
  });

  it("covers every workspace page, but leaves profile reachable", async () => {
    mockApi({ me: NONE, access: { snapshot: { admin: false, grants: [] } } });
    const a = renderAt(<AppRoutes />, "/settings/workspace");
    expect(await screen.findByText("You do not have access to any workspace")).toBeInTheDocument();
    a.unmount();
    renderAt(<AppRoutes />, "/settings/profile");
    expect(await screen.findByRole("heading", { level: 2, name: "Profile" })).toBeInTheDocument();
    expect(screen.queryByText("You do not have access to any workspace")).toBeNull();
  });

  it("does not keep refetching the workspace list looking for a missing id", async () => {
    const api = mockApi({ me: NONE, access: { snapshot: { admin: false, grants: [] } } });
    renderAt(<AppRoutes />, "/settings/workspace");
    await screen.findByText("You do not have access to any workspace");
    expect(api.callsTo("GET", "/api/workspaces").length).toBeLessThanOrEqual(1);
    expect(api.callsTo("GET", "/api/me")).toHaveLength(1);
  });
});
