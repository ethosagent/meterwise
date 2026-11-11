import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { RouterSettings, RouterStats } from "../../contract/generated";
import { DEFAULT_PAIRS, ROUTER_SETTINGS, ROUTER_STATS } from "../../test/fixtures/router";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { RouterPanel } from "./RouterPanel";

const settings = (over: Partial<RouterSettings["config"]> = {}, rest: Partial<RouterSettings> = {}): RouterSettings => ({
  ...ROUTER_SETTINGS,
  ...rest,
  config: { ...ROUTER_SETTINGS.config, mode: "conservative", ...over },
});

function show(s: RouterSettings, stats: RouterStats = ROUTER_STATS, readOnly = false) {
  const api = mockApi({ features: { router: s, routerStats: stats } });
  renderAt(<RouterPanel workspaceId="ws_1" settings={s} readOnly={readOnly} canStats />);
  return api;
}

describe("Router panel", () => {
  it("shows what routing saved and how many routed calls the provider refused", async () => {
    show(settings());
    expect(await screen.findByText(/Saved \$0\.02 over the last 7 days: 10 of 40 calls \(25%\)/)).toBeInTheDocument();
    expect(screen.getByText(/1 routed call was refused by the provider/)).toBeInTheDocument();
    expect(screen.queryByText(/trip monitors more often/)).not.toBeInTheDocument();
  });

  it("in observe it says what it would save and that nothing was rerouted", async () => {
    show(settings({ mode: "observe" }), {
      ...ROUTER_STATS,
      mode: "observe",
      routedCalls: 0,
      observedCalls: 12,
      savedUsd: "0.000000",
      wouldSaveUsd: "0.031000",
      unavailable: 0,
    });
    expect(await screen.findByText(/Would save \$0\.03 over the last 7 days on 12 calls\. Nothing was rerouted/)).toBeInTheDocument();
  });

  it("warns when the guardrail does", async () => {
    show(settings(), {
      ...ROUTER_STATS,
      guardrail: { ...ROUTER_STATS.guardrail, cheapFireBp: 800, strongFireBp: 400, warn: true },
    });
    expect(await screen.findByText("Routed calls trip monitors more often than the others")).toBeInTheDocument();
    expect(screen.getByText(/fired on 8% of routed calls against 4% of the others/)).toBeInTheDocument();
  });

  it("sends the threshold, and 0 (the mode default) when it is cleared", async () => {
    const api = show(settings({ thresholdBp: 3000 }));
    const user = userEvent.setup();
    const box = screen.getByRole("spinbutton", { name: "Routing threshold" });
    await user.clear(box);
    await user.type(box, "4000");
    await user.click(screen.getByRole("button", { name: "Save threshold" }));
    await waitFor(() => expect(api.callsTo("PATCH", /\/router$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /\/router$/)[0]?.body).toEqual({ thresholdBp: 4000 });
    await user.clear(box);
    await user.click(screen.getByRole("button", { name: "Save threshold" }));
    await waitFor(() => expect(api.callsTo("PATCH", /\/router$/)).toHaveLength(2));
    expect(api.callsTo("PATCH", /\/router$/)[1]?.body).toEqual({ thresholdBp: 0 });
  });

  it("removes a pair by sending the map without it", async () => {
    const api = show(settings());
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Remove openai: gpt-4.1 → gpt-4.1-mini" }));
    await waitFor(() => expect(api.callsTo("PATCH", /\/router$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /\/router$/)[0]?.body).toEqual({ map: [DEFAULT_PAIRS[1]] });
  });

  it("adds a suggested pair to the effective map and can reset to the defaults", async () => {
    const suggested = { provider: "openai", from: "gpt-4o", to: "gpt-4o-mini" };
    const api = show(
      settings({ map: [DEFAULT_PAIRS[0] as never] }, { effective: [DEFAULT_PAIRS[0] as never], suggestions: [suggested] }),
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("combobox", { name: "Add a model pair" }));
    await user.click(await screen.findByText("openai: gpt-4o → gpt-4o-mini (seen in your calls)"));
    await user.click(screen.getByRole("button", { name: "Add pair" }));
    await waitFor(() => expect(api.callsTo("PATCH", /\/router$/)).toHaveLength(1));
    expect(api.callsTo("PATCH", /\/router$/)[0]?.body).toEqual({ map: [DEFAULT_PAIRS[0], suggested] });
    await user.click(screen.getByRole("button", { name: "Reset to defaults" }));
    await waitFor(() => expect(api.callsTo("PATCH", /\/router$/)).toHaveLength(2));
    expect(api.callsTo("PATCH", /\/router$/)[1]?.body).toEqual({ resetMap: true });
  });

  it("without monitor_config:write every control is disabled", async () => {
    show(settings({ map: [DEFAULT_PAIRS[0] as never] }), ROUTER_STATS, true);
    await screen.findByText(/Saved \$0\.02/);
    expect(screen.getByRole("spinbutton", { name: "Routing threshold" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Reset to defaults" })).toBeDisabled();
    for (const b of screen.getAllByRole("button", { name: /^Remove / })) expect(b).toBeDisabled();
  });

  it("asks for no figures while routing is off", async () => {
    const api = show(settings({ mode: "off" }));
    await screen.findByRole("table");
    expect(api.callsTo("GET", /router\/stats/)).toHaveLength(0);
  });
});
