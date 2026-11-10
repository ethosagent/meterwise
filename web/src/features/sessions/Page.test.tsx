import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { PermissionSnapshot } from "../../contract/generated";
import { SESSIONS } from "../../test/fixtures/sessions";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { SessionsPage } from "./Page";

const PATH = "/sessions";
const NO_SESSIONS: PermissionSnapshot = {
  admin: false,
  grants: MEMBER_SNAPSHOT.grants.filter((g) => g.resourceType !== "session"),
};

describe("SessionsPage", () => {
  it("lists sessions with money in separate columns and links each to its drill-down", async () => {
    mockApi();
    renderAt(<SessionsPage />, PATH);
    const first = await screen.findByRole("link", { name: "2026-11-09 10:01:00" });
    expect(first.getAttribute("href")).toMatch(/^\/sessions\/sess_\d$/);
    const row = first.closest("tr") as HTMLElement;
    expect(within(row).getByText("opencode")).toBeInTheDocument();
    expect(within(row).getByText("$0.80")).toBeInTheDocument();
    expect(within(row).getByText("$0.25")).toBeInTheDocument();
    expect(within(row).getByText("$0.10")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("$0.35"); // saved + likely is never shown
    expect(screen.getByText("unknown")).toBeInTheDocument(); // session 2 has no client
    expect(screen.getByText("unpriced")).toBeInTheDocument(); // session 3 has no price
    expect(SESSIONS).toHaveLength(3);
  });

  it("sends the range as RFC 3339 bounds and the filters from the URL", async () => {
    const api = mockApi();
    renderAt(<SessionsPage />, `${PATH}?client=opencode&outcome=completed`);
    await screen.findByRole("link", { name: "2026-11-09 10:01:00" });
    const path = api.callsTo("GET", /\/sessions\?/)[0]?.path ?? "";
    expect(path).toContain("client=opencode");
    expect(path).toContain("outcome=completed");
    expect(path).toMatch(/from=\d{4}-\d{2}-\d{2}T00%3A00%3A00Z/);
    expect(path).toMatch(/to=\d{4}-\d{2}-\d{2}T00%3A00%3A00Z/);
    expect(path).toContain("limit=50");
    expect(screen.getAllByRole("link", { name: /^2026-/ })).toHaveLength(1);
  });

  it("choosing a filter refetches with it", async () => {
    const api = mockApi();
    renderAt(<SessionsPage />, PATH);
    await screen.findAllByRole("link", { name: /^2026-/ });
    expect(screen.getAllByRole("link", { name: /^2026-/ })).toHaveLength(3);
    await userEvent.click(screen.getByLabelText("Outcome"));
    await userEvent.click(await screen.findByTitle("abandoned"));
    await waitFor(() =>
      expect(api.callsTo("GET", /\/sessions\?.*outcome=abandoned/)).not.toHaveLength(0),
    );
    await waitFor(() => expect(screen.getAllByRole("link", { name: /^2026-/ })).toHaveLength(1));
  });

  it("an unknown outcome in the URL is ignored", async () => {
    const api = mockApi();
    renderAt(<SessionsPage />, `${PATH}?outcome=bogus`);
    await screen.findAllByRole("link", { name: /^2026-/ });
    expect(api.callsTo("GET", /outcome=/)).toEqual([]);
  });

  it("empty: no sessions links to Setup; filtered-out says so", async () => {
    mockApi({ savings: { sessions: [] } });
    const a = renderAt(<SessionsPage />, PATH);
    expect(await screen.findByText("No sessions in this range yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to Setup" })).toHaveAttribute("href", "/setup");
    a.unmount();
    mockApi({ savings: { sessions: [] } });
    renderAt(<SessionsPage />, `${PATH}?client=curl`);
    expect(await screen.findByText("No sessions match these filters")).toBeInTheDocument();
  });

  it("without session:read shows no access and makes no sessions request", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: NO_SESSIONS } });
    renderAt(<SessionsPage />, PATH);
    expect(await screen.findByText("You don't have access to sessions")).toBeInTheDocument();
    expect(api.callsTo("GET", /sessions/)).toEqual([]);
  });
});
