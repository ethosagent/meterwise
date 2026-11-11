import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { SAVER_DETAIL } from "../../test/fixtures/sessions";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { RouteEvent } from "./RouteEvent";

const routed = SAVER_DETAIL.calls.find((c) => c.routerAction === "routed");
const observed = SAVER_DETAIL.calls.find((c) => c.routerAction === "observe");

describe("Route event", () => {
  it("shows the pair, the money and the score of a routed call", () => {
    mockApi();
    if (!routed) throw new Error("fixture has no routed call");
    renderAt(<RouteEvent call={routed} workspaceId="ws_1" sessionId="sess_1" />);
    expect(screen.getByText("Routed")).toBeInTheDocument();
    expect(screen.getByText("gpt-4.1 → gpt-4.1-mini")).toBeInTheDocument();
    expect(screen.getByText(/saved \$0\.0030 vs gpt-4\.1/)).toBeInTheDocument();
    expect(screen.getByText("score 12")).toBeInTheDocument();
  });

  it("thumbs-down sends worse, shows pressed, and clears with a second click", async () => {
    const api = mockApi();
    if (!routed) throw new Error("fixture has no routed call");
    renderAt(<RouteEvent call={routed} workspaceId="ws_1" sessionId="sess_1" />);
    const user = userEvent.setup();
    const button = screen.getByRole("button", { name: "Mark routed answer worse" });
    await user.click(button);
    await waitFor(() => expect(button).toHaveAttribute("aria-pressed", "true"));
    const posts = () => api.callsTo("POST", /router-feedback$/);
    expect(posts()[0]?.body).toEqual({ verdict: "worse" });
    expect(api.features.verdictOf(routed.id)).toBe("worse");
    await user.click(button);
    await waitFor(() => expect(button).toHaveAttribute("aria-pressed", "false"));
    expect(posts()[1]?.body).toEqual({ verdict: null });
  });

  it("an observe call says would route and has no feedback button", () => {
    mockApi();
    if (!observed) throw new Error("fixture has no observe call");
    renderAt(<RouteEvent call={observed} workspaceId="ws_1" sessionId="sess_1" />);
    expect(screen.getByText("Would route")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Mark routed answer worse" }),
    ).not.toBeInTheDocument();
  });
});
