import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { act } from "react";
import { describe, expect, it, vi } from "vitest";
import { SETUP_POLL_MS, useAuditLog } from "../../api/setup";
import type { PermissionSnapshot } from "../../contract/generated";
import { AppRoutes } from "../../routes";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { ADMIN_ME, MEMBER_ME, mockApi, TEST_WORKSPACES } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { SetupPage } from "./Page";

const PATH = "/setup";
const URL_1 = TEST_WORKSPACES[0]?.proxyUrl ?? "";
const NO_URL: PermissionSnapshot = {
  admin: false,
  grants: MEMBER_SNAPSHOT.grants.filter((g) => g.resourceType !== "workspace_url"),
};
const URL_READ_ONLY = MEMBER_SNAPSHOT; // read on every workspace-scoped resource, no manage

describe("SetupPage", () => {
  it("shows the URL, the note and the five snippets with the URL filled in", async () => {
    const api = mockApi();
    renderAt(<SetupPage />, PATH);
    expect(await screen.findByLabelText("Proxy URL")).toHaveValue(URL_1);
    expect(screen.getByText(/Keep using your own provider key/)).toBeInTheDocument();
    for (const [tab, text] of [
      ["OpenAI SDK", `fixture-openai ${URL_1}`],
      ["Anthropic SDK", `fixture-anthropic ${URL_1}`],
      ["OpenCode", `fixture-opencode ${URL_1}`],
      ["Claude Code", `fixture-claude-code ${URL_1}`],
      ["curl", `fixture-curl ${URL_1}`],
    ] as const) {
      await userEvent.click(await screen.findByRole("tab", { name: tab }));
      expect(await screen.findByLabelText(`${tab} snippet`)).toHaveTextContent(text, {
        normalizeWhitespace: true,
      });
    }
    expect(api.callsTo("GET", "/api/workspaces/ws_1/setup").length).toBeGreaterThan(0);
  });

  it("copies a snippet", async () => {
    mockApi();
    const user = userEvent.setup();
    renderAt(<SetupPage />, PATH);
    await user.click(await screen.findByRole("button", { name: "Copy OpenAI SDK snippet" }));
    expect(await navigator.clipboard.readText()).toBe(`fixture-openai ${URL_1}`);
  });

  it("waits for the first call, then reads connected with a relative time and the day's count", async () => {
    mockApi();
    const a = renderAt(<SetupPage />, PATH);
    expect(await screen.findByText("waiting for first call…")).toBeInTheDocument();
    a.unmount();
    const last = new Date(Date.now() - 120_000).toISOString();
    mockApi({ setupStatus: { firstCallAt: last, lastCallAt: last, callsToday: 5 } });
    renderAt(<SetupPage />, PATH);
    expect(
      await screen.findByText("connected · last call 2 minutes ago · 5 calls today"),
    ).toBeInTheDocument();
  });

  it("without workspace_url:read the page says the URL is hidden and asks nothing of the setup endpoint", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: NO_URL } });
    renderAt(<SetupPage />, PATH);
    expect(await screen.findByText("The proxy URL is hidden for your account")).toBeInTheDocument();
    expect(screen.getByText(/Ask an organization admin/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Proxy URL")).toBeNull();
    expect(screen.queryByRole("tab")).toBeNull();
    expect(api.callsTo("GET", /\/setup$/)).toHaveLength(0);
  });

  it("read without manage shows the URL but no Reset URL", async () => {
    mockApi({ me: MEMBER_ME, access: { snapshot: URL_READ_ONLY } });
    renderAt(<SetupPage />, PATH);
    expect(await screen.findByLabelText("Proxy URL")).toHaveValue(URL_1);
    expect(screen.queryByRole("button", { name: "Reset URL" })).toBeNull();
  });

  it("Reset URL explains the grace window, posts the default 10 minutes and refreshes setup, workspaces and the audit log", async () => {
    const api = mockApi({ me: ADMIN_ME });
    renderAt(<SetupPage />, PATH);
    await screen.findByLabelText("Proxy URL");
    await userEvent.click(screen.getByRole("button", { name: "Reset URL" }));
    expect(await screen.findByText(/keeps working for the grace period/)).toBeInTheDocument();
    const before = {
      setup: api.callsTo("GET", /\/setup$/).length,
      list: api.callsTo("GET", "/api/workspaces").length,
    };
    const pop = within(document.querySelector(".ant-popover") as HTMLElement);
    await userEvent.click(pop.getByRole("button", { name: "Reset URL" }));
    await waitFor(() =>
      expect(api.callsTo("POST", "/api/workspaces/ws_1/url/reset")).toHaveLength(1),
    );
    expect(api.callsTo("POST", "/api/workspaces/ws_1/url/reset")[0]?.body).toEqual({
      graceMinutes: 10,
    });
    await waitFor(() => expect(screen.getByLabelText("Proxy URL")).not.toHaveValue(URL_1));
    expect(api.callsTo("GET", /\/setup$/).length).toBeGreaterThan(before.setup);
    expect(api.callsTo("GET", "/api/workspaces").length).toBeGreaterThan(before.list);
    expect(await screen.findByText(/previous URL still works until/)).toBeInTheDocument();
  });

  it("sends a custom grace and can end the grace period early", async () => {
    const api = mockApi();
    renderAt(<SetupPage />, PATH);
    await screen.findByLabelText("Proxy URL");
    await userEvent.click(screen.getByRole("button", { name: "Reset URL" }));
    const grace = await screen.findByLabelText("Grace period (minutes)");
    await userEvent.clear(grace);
    await userEvent.type(grace, "30");
    const pop = within(document.querySelector(".ant-popover") as HTMLElement);
    await userEvent.click(pop.getByRole("button", { name: "Reset URL" }));
    await waitFor(() =>
      expect(api.callsTo("POST", "/api/workspaces/ws_1/url/reset")).toHaveLength(1),
    );
    expect(api.callsTo("POST", "/api/workspaces/ws_1/url/reset")[0]?.body).toEqual({
      graceMinutes: 30,
    });
    await userEvent.click(await screen.findByRole("button", { name: "End grace period now" }));
    await waitFor(() =>
      expect(api.callsTo("POST", "/api/workspaces/ws_1/url/retire-previous")).toHaveLength(1),
    );
    await waitFor(() => expect(screen.queryByText(/previous URL still works/)).toBeNull());
  });

  it("a workspace switch drops the grace notice and the setup follows the new workspace", async () => {
    const api = mockApi();
    renderAt(<AppRoutes />, PATH);
    await screen.findByLabelText("Proxy URL");
    await userEvent.click(screen.getByRole("button", { name: "Reset URL" }));
    await screen.findByText(/keeps working for the grace period/);
    const buttons = screen.getAllByRole("button", { name: "Reset URL" });
    await userEvent.click(buttons[buttons.length - 1] as HTMLElement);
    expect(await screen.findByText(/previous URL still works/)).toBeInTheDocument();
    await userEvent.click(screen.getByTestId("workspace-switcher"));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Staging" }));
    await waitFor(() => expect(screen.queryByText(/previous URL still works/)).toBeNull());
    await waitFor(() =>
      expect(api.callsTo("GET", "/api/workspaces/ws_2/setup")).not.toHaveLength(0),
    );
  });

  it("an audit log observer is refreshed by a reset (the audit key is invalidated)", async () => {
    const api = mockApi({ audit: [] });
    renderAt(
      <>
        <AuditObserver />
        <SetupPage />
      </>,
      PATH,
    );
    await screen.findByLabelText("Proxy URL");
    await waitFor(() => expect(api.callsTo("GET", /\/api\/audit-log/)).toHaveLength(1));
    await userEvent.click(screen.getByRole("button", { name: "Reset URL" }));
    await screen.findByText(/keeps working for the grace period/);
    const buttons = screen.getAllByRole("button", { name: "Reset URL" });
    await userEvent.click(buttons[buttons.length - 1] as HTMLElement);
    await waitFor(() => expect(api.callsTo("GET", /\/api\/audit-log/)).toHaveLength(2));
    expect(await screen.findByText("workspace.url_reset")).toBeInTheDocument();
  });

  it("polls the setup endpoint: waiting flips to connected when the poll interval elapses", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const api = mockApi();
      renderAt(<SetupPage />, PATH);
      expect(await screen.findByText("waiting for first call…")).toBeInTheDocument();
      const last = new Date(Date.now() - 60_000).toISOString();
      api.url.status = { firstCallAt: last, lastCallAt: last, callsToday: 3 };
      await act(async () => {
        await vi.advanceTimersByTimeAsync(SETUP_POLL_MS + 100);
      });
      expect(
        await screen.findByText(/connected · last call 1 minute ago · 3 calls today/),
      ).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("stops polling once a call has been seen", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const last = new Date(Date.now() - 60_000).toISOString();
      const api = mockApi({ setupStatus: { firstCallAt: last, lastCallAt: last, callsToday: 3 } });
      renderAt(<SetupPage />, PATH);
      expect(await screen.findByText(/connected · last call/)).toBeInTheDocument();
      const seen = api.callsTo("GET", "/api/workspaces/ws_1/setup").length;
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3 * SETUP_POLL_MS);
      });
      expect(api.callsTo("GET", "/api/workspaces/ws_1/setup")).toHaveLength(seen);
    } finally {
      vi.useRealTimers();
    }
  });
});

function AuditObserver() {
  const log = useAuditLog({}, true);
  return (
    <ul>
      {log.data?.pages
        .flatMap((p) => p.items)
        .map((e) => (
          <li key={e.id}>{e.action}</li>
        ))}
    </ul>
  );
}
