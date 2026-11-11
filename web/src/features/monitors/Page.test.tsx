import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { PermissionSnapshot } from "../../contract/generated";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { MonitorsPage } from "./Page";

const PATH = "/monitors";
const without = (...resources: string[]): PermissionSnapshot => ({
  admin: false,
  grants: MEMBER_SNAPSHOT.grants.filter((g) => !resources.includes(g.resourceType)),
});
/** A member who reads settings and figures but may not write them. */
const READ_ONLY = MEMBER_SNAPSHOT;

const row = async (name: string) => (await screen.findByText(name)).closest("tr") as HTMLElement;
const mode = (r: HTMLElement, label: string) => within(r).getByRole("radio", { name: label });
/** antd hides the radio input (pointer-events: none) and takes the click on its label text. */
const pick = (user: ReturnType<typeof userEvent.setup>, r: HTMLElement, label: string) =>
  user.click(within(r).getByText(label));

describe("MonitorsPage", () => {
  it("lists the six monitors with modes, fires and three separate money columns", async () => {
    mockApi();
    renderAt(<MonitorsPage />, PATH);
    const dup = await row("Duplicate tool call");
    expect(mode(dup, "Observe")).toBeChecked();
    expect(within(dup).getByText("6")).toBeInTheDocument(); // fires
    expect(within(dup).getByText("$0.70")).toBeInTheDocument(); // would save, observe mode
    const rr = await row("Repeated read");
    expect(mode(rr, "Nudge")).toBeChecked();
    expect(within(rr).getByText("$0.30")).toBeInTheDocument(); // saved
    expect(within(rr).getByText("$0.10")).toBeInTheDocument(); // likely
    expect(within(rr).getByText("2")).toBeInTheDocument(); // marked wrong
    expect(document.body.textContent).not.toContain("$0.40"); // saved + likely is never shown
    const mapRows = within(screen.getByRole("table", { name: "Model map" })).getAllByRole("row");
    const monitorRows = screen.getAllByRole("row").filter((r) => !mapRows.includes(r));
    expect(monitorRows).toHaveLength(7); // header + six
  });

  it("observe rows show would-save as an outlined amber tag", async () => {
    mockApi();
    renderAt(<MonitorsPage />, PATH);
    const dup = await row("Duplicate tool call");
    const tag = within(dup).getByText("$0.70");
    expect(tag.className).toContain("ant-tag-warning");
  });

  it("Nudge PATCHes the exact partial body", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, PATH);
    const user = userEvent.setup();
    const dup = await row("Duplicate tool call");
    await pick(user, dup, "Nudge");
    await waitFor(() => expect(api.callsTo("PATCH", /\/monitors$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /\/monitors$/)[0]?.body).toEqual({
      duplicate_tool_call: { mode: "note" },
    });
    await waitFor(() => expect(mode(dup, "Nudge")).toBeChecked());
    expect(api.savings.monitors().duplicate_tool_call.mode).toBe("note");
  });

  it("Block needs a confirm: cancel sends nothing, confirm sends the PATCH", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, PATH);
    const user = userEvent.setup();
    const rr = await row("Repeated read");
    await pick(user, rr, "Block");
    expect(await screen.findByText("Turn on Block for Repeated read?")).toBeInTheDocument();
    expect(api.callsTo("PATCH", /monitors/)).toHaveLength(0);
    expect(mode(rr, "Nudge")).toBeChecked(); // not changed until confirmed
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.getByText("Turn on Block for Repeated read?")).not.toBeVisible(),
    );
    expect(api.callsTo("PATCH", /monitors/)).toHaveLength(0);
    await pick(user, rr, "Block");
    await user.click(await screen.findByRole("button", { name: "Block" }));
    await waitFor(() => expect(api.callsTo("PATCH", /monitors$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /monitors$/)[0]?.body).toEqual({
      repeated_read: { mode: "block" },
    });
    await waitFor(() => expect(mode(rr, "Block")).toBeChecked());
  });

  it("reasoning_loop and second_guessing offer Observe only", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, PATH);
    for (const name of ["Reasoning loop", "Second guessing"]) {
      const r = await row(name);
      expect(mode(r, "Observe")).toBeChecked();
      expect(mode(r, "Nudge")).toBeDisabled();
      expect(mode(r, "Block")).toBeDisabled();
    }
    expect(api.callsTo("PATCH", /monitors/)).toHaveLength(0);
    await userEvent.hover(within(await row("Reasoning loop")).getByText("Nudge"));
    expect(await screen.findByText("observe-only until tuned")).toBeInTheDocument();
  });

  it("turn on switches an observe monitor with would-save to Nudge", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, PATH);
    const dup = await row("Duplicate tool call");
    await userEvent.click(within(dup).getByRole("button", { name: "Turn on" }));
    await waitFor(() =>
      expect(api.callsTo("PATCH", /monitors$/)[0]?.body).toEqual({
        duplicate_tool_call: { mode: "note" },
      }),
    );
  });

  it("edits thresholds and sends only the changed field", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, PATH);
    const user = userEvent.setup();
    await row("Duplicate tool call");
    await user.click(screen.getByRole("button", { name: "Duplicate tool call thresholds" }));
    const drawer = await screen.findByRole("dialog");
    const input = within(drawer).getByLabelText("Max repeats");
    expect(input).toHaveValue("3");
    const save = within(drawer).getByRole("button", { name: "Save" });
    expect(save).toBeDisabled(); // nothing changed
    await user.clear(input);
    await user.type(input, "5");
    await user.click(save);
    await waitFor(() => expect(api.callsTo("PATCH", /monitors$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /monitors$/)[0]?.body).toEqual({
      duplicate_tool_call: { maxRepeats: 5 },
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.savings.monitors().duplicate_tool_call.maxRepeats).toBe(5);
    expect(api.savings.monitors().duplicate_tool_call.lookBack).toBe(20); // untouched
  });

  it("the stats range lives in the URL", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, `${PATH}?range=30d`);
    await row("Duplicate tool call");
    expect(api.callsTo("GET", /monitors\/stats\?range=30d$/)).toHaveLength(1);
    await userEvent.click(screen.getByText("7 days"));
    await waitFor(() => expect(api.callsTo("GET", /monitors\/stats\?range=7d$/)).toHaveLength(1));
  });
});

describe("MonitorsPage permissions", () => {
  it("without monitor_config:write the modes are disabled and the page says why", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: READ_ONLY } });
    renderAt(<MonitorsPage />, PATH);
    const rr = await row("Repeated read");
    expect(mode(rr, "Block")).toBeDisabled();
    expect(mode(rr, "Observe")).toBeDisabled();
    expect(screen.getByText(/needs monitor_config:write/)).toBeInTheDocument();
    expect(within(rr).queryByRole("button", { name: "Turn on" })).toBeNull();
    expect(api.callsTo("PATCH", /monitors/)).toHaveLength(0);
  });

  it("without monitor_config:read shows no access and asks for nothing", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: without("monitor_config") } });
    renderAt(<MonitorsPage />, PATH);
    expect(
      await screen.findByText("You don't have access to monitor settings"),
    ).toBeInTheDocument();
    expect(api.callsTo("GET", /monitors/)).toEqual([]);
  });

  it("without savings:read the settings show but the figures are dashes and stats are not requested", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: without("savings") } });
    renderAt(<MonitorsPage />, PATH);
    const rr = await row("Repeated read");
    expect(within(rr).getAllByText("-").length).toBeGreaterThanOrEqual(4);
    expect(api.callsTo("GET", /monitors\/stats/)).toEqual([]);
    expect(api.callsTo("GET", /monitors$/)).toHaveLength(1);
  });
});
