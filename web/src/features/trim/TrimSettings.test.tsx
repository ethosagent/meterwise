import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { stubTrimRoutes, TRIM_STATS_ACTED, TRIM_STATS_OBSERVE } from "../../test/fixtures/trim";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { MonitorsPage } from "../monitors/Page";

const card = async () => within(await screen.findByLabelText("Savers"));

describe("Trimming settings", () => {
  it("observe shows what it would have saved, as its own number", async () => {
    mockApi();
    stubTrimRoutes(TRIM_STATS_OBSERVE);
    renderAt(<MonitorsPage />, "/monitors");
    const c = await card();
    expect(
      await c.findByText(/Would save \$0\.30 \(−3,200 tokens\) in the last 7 days/),
    ).toBeInTheDocument();
    expect(c.queryByText(/^Saved /)).not.toBeInTheDocument();
  });

  it("a lossless workspace shows saved and likely saved apart", async () => {
    mockApi({
      features: {
        trim: { mode: "lossless", neverTrimTools: [], maxFoldPct: 90, minGainPct: 2, stages: {} },
      },
    });
    stubTrimRoutes(TRIM_STATS_ACTED);
    renderAt(<MonitorsPage />, "/monitors");
    const c = await card();
    expect(await c.findByText("Saved $2.00, likely $0.40 in the last 7 days")).toBeInTheDocument();
    expect(c.queryByText(/Would save/)).not.toBeInTheDocument();
  });

  it("Aggressive waits behind a confirm; Lossless does not", async () => {
    const api = mockApi();
    stubTrimRoutes(TRIM_STATS_OBSERVE);
    renderAt(<MonitorsPage />, "/monitors");
    const user = userEvent.setup();
    const c = await card();
    await user.click(c.getAllByText("Aggressive")[0] as HTMLElement);
    expect(await screen.findByText("Turn on Aggressive trimming?")).toBeInTheDocument();
    expect(api.callsTo("PATCH", /\/trim$/)).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "Aggressive" }));
    await waitFor(() => expect(api.callsTo("PATCH", /\/trim$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /\/trim$/)[0]?.body).toEqual({ mode: "aggressive" });
  });

  it("the settings drawer sends only what changed: a stage, a never-trim tool and the max fold", async () => {
    const api = mockApi();
    stubTrimRoutes(TRIM_STATS_ACTED);
    renderAt(<MonitorsPage />, "/monitors");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Settings" }));
    const d = within(await screen.findByRole("dialog"));
    expect(d.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(await d.findByText("run_shell")).toBeInTheDocument(); // the by-tool table
    await user.click(d.getByRole("switch", { name: "Long tool descriptions" }));
    const fold = d.getByRole("spinbutton", { name: /Most of a result/ });
    await user.clear(fold);
    await user.type(fold, "70");
    await user.type(d.getByRole("combobox"), "read_file{Enter}");
    await user.click(d.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(api.callsTo("PATCH", /\/trim$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /\/trim$/)[0]?.body).toEqual({
      maxFoldPct: 70,
      neverTrimTools: ["read_file"],
      stages: { schema_trim: true },
    });
  });

  it("without monitor_config:write the drawer is read-only", async () => {
    const api = mockApi({ access: { snapshot: MEMBER_SNAPSHOT } });
    stubTrimRoutes(TRIM_STATS_ACTED);
    renderAt(<MonitorsPage />, "/monitors");
    await userEvent.setup().click(await screen.findByRole("button", { name: "Settings" }));
    const d = within(await screen.findByRole("dialog"));
    expect(d.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(d.getByRole("switch", { name: "Duplicate lines" })).toBeDisabled();
    expect(api.callsTo("PATCH", /\/trim$/)).toHaveLength(0);
  });
});
