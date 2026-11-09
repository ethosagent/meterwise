import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it } from "vitest";
import { AppRoutes } from "../routes";
import { useUi } from "../state/ui";
import { errorResponse, mockApi } from "../test/mockApi";
import { testQueryClient } from "../test/render";
import { installMatchMedia } from "../test/setup";
import { ThemeProvider } from "../theme/ThemeProvider";
import { useTheme } from "../theme/useTheme";
import { FALLBACK_BRAND } from "./Brand";
import { ALL_PAGES } from "./nav";

function renderApp(path: string) {
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <ThemeProvider>
        <MemoryRouter initialEntries={[path]}>
          <AppRoutes />
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  useTheme.setState({ mode: "system" });
  localStorage.clear();
  useUi.setState({ siderCollapsed: false, activeWorkspaceId: "ws-default" });
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
    for (const label of ["Profile", "Workspace", "Organization", "Access management"]) {
      expect(within(nav).getByText(label)).toBeInTheDocument();
    }
  });

  it("renders the right head and an Empty body on every route", async () => {
    mockApi();
    for (const page of ALL_PAGES) {
      const { unmount } = renderApp(page.path);
      expect(
        await screen.findByRole("heading", { level: 2, name: page.label }),
      ).toBeInTheDocument();
      expect(screen.getByText("Nothing here yet")).toBeInTheDocument();
      unmount();
    }
  });

  it("navigates when a menu item is clicked", async () => {
    mockApi();
    renderApp("/overview");
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
    mockApi();
    renderApp("/overview");
    const sw = await screen.findByTestId("workspace-switcher");
    expect(sw).toHaveTextContent("Default workspace");
    expect(screen.queryByText(/organization/i, { selector: "button *" })).toBeNull();
    await userEvent.click(sw);
    await userEvent.click(await screen.findByText("Staging"));
    expect(useUi.getState().activeWorkspaceId).toBe("ws-staging");
    expect(screen.getByTestId("workspace-switcher")).toHaveTextContent("Staging");
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
    expect(within(header).getByLabelText("Account menu")).toHaveTextContent("DU");
  });
});

describe("Theme toggle", () => {
  it("applies the choice at once and PATCHes the full preferences object", async () => {
    const api = mockApi({ preferences: { theme: "system", reducedMotion: true } });
    renderApp("/overview");
    await screen.findByTestId("brand-name");
    await waitFor(() =>
      expect(api.fetch).toHaveBeenCalledWith("/api/me/preferences", expect.anything()),
    );
    await userEvent.click(screen.getByRole("img", { name: "Dark" }));
    expect(useTheme.getState().mode).toBe("dark");
    await waitFor(() => expect(api.patches).toEqual([{ theme: "dark", reducedMotion: true }]));
    expect(document.documentElement.dataset.theme).toBe("dark");
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
