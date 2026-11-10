import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { PermissionSnapshot } from "../../contract/generated";
import {
  OVERVIEW_ACTIVE,
  OVERVIEW_COLLECTING,
  OVERVIEW_NO_CALLS,
  saveRow,
} from "../../test/fixtures/savings";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { OverviewPage } from "./Page";

const PATH = "/overview";
const NO_SAVINGS: PermissionSnapshot = {
  admin: false,
  grants: MEMBER_SNAPSHOT.grants.filter((g) => g.resourceType !== "savings"),
};

describe("OverviewPage hero", () => {
  it("shows the certain figure as the hero and likely on its own under a dashed rule, never summed", async () => {
    mockApi();
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByTestId("hero-saved")).toHaveTextContent("$1.00");
    expect(screen.getByTestId("hero-share")).toHaveTextContent("10%");
    expect(screen.getByTestId("hero-tokens")).toHaveTextContent("120,000");
    expect(screen.getByTestId("hero-likely")).toHaveTextContent("Likely saved");
    expect(screen.getByTestId("hero-likely")).toHaveTextContent("$0.50");
    expect(document.querySelector(".ant-divider-dashed")).not.toBeNull();
    // 1.00 + 0.50 must appear nowhere (ADR 0023), and the hero is not the likely number.
    expect(document.body.textContent).not.toContain("$1.50");
    expect(screen.getByTestId("hero-saved")).not.toHaveTextContent("$0.50");
  });

  it("names unpriced calls instead of hiding them", async () => {
    mockApi({
      savings: {
        overview: { ...OVERVIEW_ACTIVE, hero: { ...OVERVIEW_ACTIVE.hero, unpricedCalls: 2 } },
      },
    });
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByText(/2 calls have no price on file/)).toBeInTheDocument();
  });

  it("shows n/a for the share when the server has no denominator", async () => {
    mockApi({
      savings: {
        overview: { ...OVERVIEW_ACTIVE, hero: { ...OVERVIEW_ACTIVE.hero, savedBp: null } },
      },
    });
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByTestId("hero-share")).toHaveTextContent("n/a");
  });
});

describe("OverviewPage causes and chart", () => {
  it("draws certain, likely and would-save bars in three distinct styles", async () => {
    mockApi();
    renderAt(<OverviewPage />, PATH);
    const dup = await screen.findByTestId("cause-duplicate_tool_call");
    const certain = dup.querySelector<HTMLElement>('[data-kind="certain"]');
    const likely = screen
      .getByTestId("cause-no_progress")
      .querySelector<HTMLElement>('[data-kind="likely"]');
    const would = screen
      .getByTestId("cause-reasoning_loop")
      .querySelector<HTMLElement>('[data-kind="would-save"]');
    expect(certain).not.toBeNull();
    expect(likely).not.toBeNull();
    expect(would).not.toBeNull();
    const styles = [certain, likely, would].map((el) => {
      const s = (el as HTMLElement).style;
      return `${s.background}|${s.border}`;
    });
    expect(new Set(styles).size).toBe(3);
    expect(within(dup).getByLabelText("saved $0.70")).toBeInTheDocument();
    expect(
      within(screen.getByTestId("cause-reasoning_loop")).getByText(/\$0\.20 would save if enabled/),
    ).toBeInTheDocument();
    expect(
      within(screen.getByTestId("cause-step_budget")).getByText("Nothing yet"),
    ).toBeInTheDocument();
  });

  it("labels the spend chart region", async () => {
    mockApi();
    renderAt(<OverviewPage />, PATH);
    expect(
      await screen.findByRole("img", { name: /Spend by UTC day, 2026-11-04 to 2026-11-10/ }),
    ).toBeInTheDocument();
  });

  it("asks for the biggest saves while the overview is still loading (no waterfall)", async () => {
    const api = mockApi();
    const served = globalThis.fetch;
    vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) =>
      /\/savings\/overview/.test(String(input))
        ? new Promise<Response>(() => undefined)
        : served(input, init),
    );
    renderAt(<OverviewPage />, PATH);
    await waitFor(() => expect(api.callsTo("GET", /\/savings\/saves\?/)).toHaveLength(1));
    expect(screen.queryByTestId("hero-saved")).toBeNull(); // the overview is still pending
  });

  it("offers the observe-mode upsell and explains how it counts", async () => {
    mockApi();
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByText("Reasoning loop would save $0.20")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Monitors" })).toHaveAttribute(
      "href",
      "/monitors",
    );
    const how = screen.getByLabelText("How we count");
    expect(within(how).getByText(/answered by the proxy/)).toBeInTheDocument();
    expect(within(how).getByText(/never added to it/)).toBeInTheDocument();
  });
});

describe("OverviewPage biggest saves", () => {
  it("links each row to its session step by event id and tags certain and likely", async () => {
    mockApi();
    renderAt(<OverviewPage />, PATH);
    const table = await screen.findByLabelText("Biggest saves");
    const links = await within(table).findAllByRole("link");
    expect(links.map((a) => a.getAttribute("href"))).toEqual([
      "/sessions/sess_1?event=ev_1",
      "/sessions/sess_2?event=ev_2",
      "/sessions/sess_3?event=ev_3",
    ]);
    expect(within(table).getAllByText("certain", { selector: ".ant-tag" })).toHaveLength(2);
    expect(within(table).getByText("likely")).toBeInTheDocument();
    expect(within(table).getByText("unknown")).toBeInTheDocument(); // a NULL client
    expect(within(table).getByText("Open call #1")).toBeInTheDocument();
    expect(within(table).getByText("Open step")).toBeInTheDocument(); // call index 0 after a restart
  });

  it("pages with the cursor", async () => {
    const saves = Array.from({ length: 25 }, (_, i) => saveRow(i + 1));
    const api = mockApi({ savings: { saves } });
    renderAt(<OverviewPage />, PATH);
    const table = await screen.findByLabelText("Biggest saves");
    await waitFor(() => expect(within(table).getAllByRole("link")).toHaveLength(20));
    await userEvent.click(within(table).getByRole("button", { name: "Load more" }));
    await waitFor(() => expect(within(table).getAllByRole("link")).toHaveLength(25));
    expect(within(table).queryByRole("button", { name: "Load more" })).toBeNull();
    const paths = api.callsTo("GET", /savings\/saves/).map((c) => c.path);
    expect(paths[0]).toContain("limit=20");
    expect(paths[1]).toContain("cursor=20");
  });

  it("says so when nothing has been saved yet in an active range", async () => {
    mockApi({ savings: { saves: [] } });
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByText("No saves in this range yet")).toBeInTheDocument();
  });
});

describe("OverviewPage range and tab in the URL", () => {
  it("sends the range from the URL and switches it by writing the URL", async () => {
    const api = mockApi();
    renderAt(<OverviewPage />, `${PATH}?range=30d`);
    await screen.findByTestId("hero-saved");
    expect(api.callsTo("GET", /savings\/overview\?range=30d$/)).toHaveLength(1);
    await userEvent.click(screen.getByText("7 days"));
    await waitFor(() => expect(api.callsTo("GET", /savings\/overview\?range=7d$/)).toHaveLength(1));
  });

  it("applies a valid custom range from the URL", async () => {
    const api = mockApi();
    renderAt(<OverviewPage />, `${PATH}?range=custom&from=2026-11-01&to=2026-11-05`);
    await screen.findByTestId("hero-saved");
    expect(
      api.callsTo("GET", /overview\?range=custom&from=2026-11-01&to=2026-11-05$/),
    ).toHaveLength(1);
    expect(screen.getByLabelText("From")).toHaveValue("2026-11-01");
    expect(screen.getByLabelText("To")).toHaveValue("2026-11-05");
    expect(screen.getByText("Days are UTC.")).toBeInTheDocument();
  });

  it("falls back to 7d for a custom range the server would refuse", async () => {
    const api = mockApi();
    renderAt(<OverviewPage />, `${PATH}?range=custom&from=2026-11-05&to=2026-11-01`);
    await screen.findByTestId("hero-saved");
    expect(api.callsTo("GET", /overview\?range=7d$/)).toHaveLength(1);
  });

  it("opens the causes tab from ?tab=causes with cards, clients and a copyable suggestion", async () => {
    const api = mockApi();
    const user = userEvent.setup();
    renderAt(<OverviewPage />, `${PATH}?tab=causes`);
    const card = await screen.findByTestId("cause-card-duplicate_tool_call");
    expect(card).toHaveTextContent("$0.70");
    expect(card).toHaveTextContent("Top client opencode");
    expect(card).toHaveTextContent("src/main.go (4x)");
    // no stored path: the tool and the hash prefix
    expect(screen.getByTestId("cause-card-repeated_read")).toHaveTextContent("grep #ab12cd34");
    const clients = screen.getByLabelText("By client");
    expect(within(clients).getByText("unknown")).toBeInTheDocument();
    expect(within(clients).getByText("10.45%")).toBeInTheDocument();
    expect(within(clients).getByText(/-2\.5 vs the window before/)).toBeInTheDocument();
    expect(within(clients).getByText(/no earlier window/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Copy suggestion" }));
    expect(await navigator.clipboard.readText()).toMatch(/read_file read src\/main\.go 4 times/);
    expect(api.callsTo("GET", /savings\/causes/)).toHaveLength(1);
    expect(api.callsTo("GET", /savings\/overview/)).toHaveLength(0);
  });

  it("switching tabs keeps working and the range", async () => {
    mockApi();
    renderAt(<OverviewPage />, `${PATH}?range=30d`);
    await screen.findByTestId("hero-saved");
    await userEvent.click(screen.getByRole("tab", { name: /Where it came/ }));
    expect(await screen.findByTestId("cause-card-repeated_read")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("tab", { name: "Summary" }));
    expect(await screen.findByTestId("hero-saved")).toBeInTheDocument();
  });
});

describe("OverviewPage empty states and access", () => {
  it("no_calls links to Setup and shows no figures", async () => {
    mockApi({ savings: { overview: OVERVIEW_NO_CALLS } });
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByText("Waiting for your first call")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to Setup" })).toHaveAttribute("href", "/setup");
    expect(screen.queryByTestId("hero-saved")).toBeNull();
  });

  it("collecting says the monitors are observing and shows would-save", async () => {
    mockApi({ savings: { overview: OVERVIEW_COLLECTING } });
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByText(/Collecting: your monitors are observing/)).toBeInTheDocument();
    expect(screen.getByText(/Observe mode found \$0\.40/)).toBeInTheDocument();
    expect(screen.getByTestId("hero-saved")).toHaveTextContent("$0.00");
    expect(screen.getByText("Duplicate tool call would save $0.40")).toBeInTheDocument();
  });

  it("without savings:read shows no access and asks for no savings data", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: NO_SAVINGS } });
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByText("You don't have access to savings")).toBeInTheDocument();
    expect(screen.getByText(/savings:read permission/)).toBeInTheDocument();
    expect(api.callsTo("GET", /savings/)).toEqual([]);
    expect(screen.queryByTestId("hero-saved")).toBeNull();
  });

  it("a member with savings:read sees the page", async () => {
    mockApi({ me: MEMBER_ME });
    renderAt(<OverviewPage />, PATH);
    expect(await screen.findByTestId("hero-saved")).toHaveTextContent("$1.00");
  });
});
