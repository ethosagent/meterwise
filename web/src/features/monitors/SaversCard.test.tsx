import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { MonitorsPage } from "./Page";

const card = async () => within(await screen.findByLabelText("Savers"));

describe("Savers card", () => {
  it("shows trimming at its default Observe, routing and memory Off, and the memory reason", async () => {
    mockApi();
    renderAt(<MonitorsPage />, "/monitors");
    const c = await card();
    const off = c.getAllByRole("radio", { name: "Off" });
    expect(off).toHaveLength(3);
    expect(off[0]).not.toBeChecked();
    expect(c.getAllByRole("radio", { name: "Observe" })[0]).toBeChecked();
    expect(off[1]).toBeChecked();
    expect(off[2]).toBeChecked();
    expect(c.getByText(/no memory model key/)).toBeInTheDocument();
  });

  it("trimming PATCHes its mode straight away", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, "/monitors");
    const user = userEvent.setup();
    const c = await card();
    await user.click(c.getAllByText("Lossless")[0] as HTMLElement);
    await waitFor(() => expect(api.callsTo("PATCH", /\/trim$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /\/trim$/)[0]?.body).toEqual({ mode: "lossless" });
    expect(api.features.trim().mode).toBe("lossless");
  });

  it("routing's acting mode waits for a confirm; cancel sends nothing", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, "/monitors");
    const user = userEvent.setup();
    const c = await card();
    await user.click(c.getAllByText("Conservative")[0] as HTMLElement);
    expect(await screen.findByText("Turn on Conservative routing?")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(api.callsTo("PATCH", /\/router$/)).toHaveLength(0);
    await user.click(c.getAllByText("Conservative")[0] as HTMLElement);
    await user.click(await screen.findByRole("button", { name: "Conservative" }));
    await waitFor(() => expect(api.callsTo("PATCH", /\/router$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /\/router$/)[0]?.body).toEqual({ mode: "conservative" });
  });

  it("observe on memory needs no confirm; hint does", async () => {
    const api = mockApi();
    renderAt(<MonitorsPage />, "/monitors");
    const user = userEvent.setup();
    const c = await card();
    await user.click(c.getByText("Hint"));
    expect(await screen.findByText("Turn on Memory hints?")).toBeInTheDocument();
    expect(api.callsTo("PATCH", /\/memory$/)).toHaveLength(0);
  });

  it("a member without monitor_config:write cannot change a mode", async () => {
    const api = mockApi({ access: { snapshot: MEMBER_SNAPSHOT } });
    renderAt(<MonitorsPage />, "/monitors");
    const c = await card();
    for (const r of c.getAllByRole("radio")) expect(r).toBeDisabled();
    expect(api.callsTo("PATCH", /\/trim$/)).toHaveLength(0);
  });
});
