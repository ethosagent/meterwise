import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { AppRoutes } from "../routes";
import { useUi } from "../state/ui";
import { errorResponse, mockApi, TEST_WORKSPACES } from "../test/mockApi";
import { renderAt } from "../test/render";
import { installMatchMedia } from "../test/setup";
import { useTheme } from "../theme/useTheme";
import { FALLBACK_BRAND } from "./Brand";
import { ALL_PAGES } from "./nav";

const renderApp = (path: string) => renderAt(<AppRoutes />, path);
/** Pages with real content; every other destination is still the empty stub. */
const BUILT = new Set([
  "workspace",
  "organization",
  "access",
  "setup",
  "overview",
  "sessions",
  "monitors",
]);
const NAMES = TEST_WORKSPACES.map((w) => w.name);

beforeEach(() => {
  useTheme.setState({ mode: "system" });
  localStorage.clear();
  useUi.setState({ siderCollapsed: false });
});

describe("Shell brand", () => {
  it("renders appName from /api/config and uses it in the tab title and footer", async () => {
    mockApi();
    renderApp("/overview");
    expect(await screen.findByTestId("brand-name")).toHaveTextContent("Acme Meter");
    await waitFor(() => expect(document.title).toBe("Overview · Acme Meter"));
    expect(screen.getByRole("contentinfo")).toHaveTextContent("Acme Meter");
  });

  it("falls back to a neutral word when /api/config fails", async () => {
    mockApi({ config: errorResponse(500, "internal", "down") });
    renderApp("/overview");
    expect(await screen.findByTestId("brand-name")).toHaveTextContent(FALLBACK_BRAND);
  });
});

describe("Shell navigation", () => {
  it("shows every destination, including the Settings group", async () => {
    mockApi();
    renderApp("/settings/profile");
    const nav = await screen.findByRole("menu", { name: "Main navigation" });
    for (const label of ["Overview", "Sessions", "Monitors", "Setup", "Settings"]) {
      expect(within(nav).getByText(label)).toBeInTheDocument();
    }
    for (const label of ["Profile", "Workspace", "Organization"]) {
      expect(within(nav).getByText(label)).toBeInTheDocument();
    }
    // Access management follows the snapshot: an admin has member:read, so it appears once loaded.
    expect(await within(nav).findByText("Access management")).toBeInTheDocument();
  });

  it("renders the right head on every route, and an Empty body on the unbuilt ones", async () => {
    mockApi();
    for (const page of ALL_PAGES) {
      const { unmount } = renderApp(page.path);
      expect(
        await screen.findByRole("heading", { level: 2, name: page.label }),
      ).toBeInTheDocument();
      if (!BUILT.has(page.key)) expect(screen.getByText("Nothing here yet")).toBeInTheDocument();
      unmount();
    }
  });

  it("navigates when a menu item is clicked", async () => {
    mockApi();
    // From Setup, not Overview: a real Overview page (chart module, tables) makes this click race the 15 s test limit under load.
    renderApp("/setup");
    await userEvent.click(await screen.findByText("Sessions"));
    expect(await screen.findByRole("heading", { level: 2, name: "Sessions" })).toBeInTheDocument();
  });

  it("redirects / to overview and /settings to profile", async () => {
    mockApi();
    const a = renderApp("/");
    expect(await screen.findByRole("heading", { level: 2, name: "Overview" })).toBeInTheDocument();
    a.unmount();
    renderApp("/settings");
    expect(await screen.findByRole("heading", { level: 2, name: "Profile" })).toBeInTheDocument();
  });

  it("shows a not-found page for an unknown address", async () => {
    mockApi();
    renderApp("/nowhere");
    expect(await screen.findByText("Page not found")).toBeInTheDocument();
  });
});

describe("Shell chrome", () => {
  it("has a workspace switcher with no organization row, and switching changes it", async () => {
    const api = mockApi();
    renderApp("/overview");
    const sw = await screen.findByTestId("workspace-switcher");
    await waitFor(() => expect(sw).toHaveTextContent("Default workspace"));
    await userEvent.click(sw);
    const staging = await screen.findByRole("menuitem", { name: "Staging" });
    const menu = staging.closest<HTMLElement>('[role="menu"]') as HTMLElement;
    const items = within(menu).getAllByRole("menuitem");
    expect(items.map((i) => i.textContent)).toEqual([...NAMES, "＋ New workspace"]);
    expect(within(menu).queryByText(/organi[sz]ation/i)).toBeNull();
    await userEvent.click(staging);
    await waitFor(() =>
      expect(screen.getByTestId("workspace-switcher")).toHaveTextContent("Staging"),
    );
    expect(api.callsTo("POST", "/api/workspaces/ws_2/switch")).toHaveLength(1);
  });

  it("shows full workspace names in the switcher popup while the sider is collapsed", async () => {
    mockApi();
    const { container } = renderApp("/overview");
    await screen.findByTestId("brand-name");
    await userEvent.click(container.querySelector(".ant-layout-sider-trigger") as Element);
    await userEvent.click(screen.getByTestId("workspace-switcher"));
    const items = await screen.findAllByRole("menuitem", { name: /workspace|Staging|Research/ });
    expect(items.map((i) => i.textContent)).toEqual([...NAMES, "＋ New workspace"]);
  });

  it("keeps the Settings group open on a settings page and closed elsewhere", async () => {
    mockApi();
    const { container } = renderApp("/overview");
    await screen.findByTestId("brand-name");
    const settingsOpen = () =>
      container.querySelector(".ant-menu-submenu-open > .ant-menu-submenu-title") !== null;
    expect(settingsOpen()).toBe(false);
    await userEvent.click(screen.getByLabelText("Account menu"));
    await userEvent.click(await screen.findByText("Profile", { selector: ".ant-dropdown *" }));
    await waitFor(() => expect(settingsOpen()).toBe(true));
  });

  it("collapses and expands the sider", async () => {
    mockApi();
    const { container } = renderApp("/overview");
    await screen.findByTestId("brand-name");
    const trigger = container.querySelector(".ant-layout-sider-trigger");
    expect(trigger).not.toBeNull();
    await userEvent.click(trigger as Element);
    expect(useUi.getState().siderCollapsed).toBe(true);
    expect(screen.queryByTestId("brand-name")).toBeNull();
    await userEvent.click(trigger as Element);
    expect(useUi.getState().siderCollapsed).toBe(false);
  });

  it("has the header controls: title, search, theme toggle, avatar", async () => {
    mockApi();
    installMatchMedia((q) => q.includes("min-width")); // a wide viewport
    renderApp("/monitors");
    const header = await screen.findByRole("banner");
    expect(within(header).getByText("Monitors")).toBeInTheDocument();
    expect(within(header).getByLabelText("Search")).toBeInTheDocument();
    expect(within(header).getByLabelText("Theme")).toBeInTheDocument();
    expect(within(header).getByLabelText("Account menu")).toHaveTextContent("AA");
  });
});

describe("Theme toggle", () => {
  it("applies the choice at once and PATCHes only the theme, leaving reducedMotion alone", async () => {
    const api = mockApi({ preferences: { theme: "system", reducedMotion: true } });
    renderApp("/overview");
    await screen.findByTestId("brand-name");
    await waitFor(() =>
      expect(api.fetch).toHaveBeenCalledWith("/api/me/preferences", expect.anything()),
    );
    await userEvent.click(screen.getByRole("img", { name: "Dark" }));
    expect(useTheme.getState().mode).toBe("dark");
    await waitFor(() => expect(api.patches).toEqual([{ theme: "dark" }]));
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("does not clobber a stored reducedMotion when the preferences GET failed", async () => {
    const api = mockApi({
      preferences: { theme: "system", reducedMotion: true },
      preferencesGetStatus: 500,
    });
    renderApp("/overview");
    await screen.findByTestId("brand-name");
    await userEvent.click(screen.getByRole("img", { name: "Dark" }));
    await waitFor(() => expect(api.patches).toEqual([{ theme: "dark" }]));
    expect(api.patches[0]).not.toHaveProperty("reducedMotion");
  });

  it("tells the person when the theme could not be saved, and keeps it on this device", async () => {
    mockApi({ preferencesPatchStatus: 500 });
    renderApp("/overview");
    await screen.findByTestId("brand-name");
    await userEvent.click(screen.getByRole("img", { name: "Dark" }));
    expect(await screen.findByText(/Could not save your theme/)).toBeInTheDocument();
    expect(useTheme.getState().mode).toBe("dark");
  });

  it("adopts the server theme when this device has no stored choice", async () => {
    mockApi({ preferences: { theme: "dark", reducedMotion: false } });
    renderApp("/overview");
    await waitFor(() => expect(useTheme.getState().mode).toBe("dark"));
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("keeps a stored local choice over the server's", async () => {
    localStorage.setItem("ui.theme", JSON.stringify({ state: { mode: "light" }, version: 0 }));
    await useTheme.persist.rehydrate();
    const api = mockApi({ preferences: { theme: "dark", reducedMotion: false } });
    renderApp("/overview");
    await waitFor(() =>
      expect(api.fetch).toHaveBeenCalledWith("/api/me/preferences", expect.anything()),
    );
    expect(useTheme.getState().mode).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
  });
});
