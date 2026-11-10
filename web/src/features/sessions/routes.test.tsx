import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { AppRoutes } from "../../routes";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";

describe("savings routes in the app", () => {
  it("a saves row opens its session step through the real routes and keeps Sessions selected", async () => {
    const api = mockApi();
    renderAt(<AppRoutes />, "/overview");
    const table = await screen.findByLabelText("Biggest saves");
    const link = await within(table).findByRole("link", { name: "Open call #1" });
    await userEvent.click(link);
    // the detail reads the session of the link, authorized by the active workspace
    const step = await screen.findByTestId("step-call_3");
    expect(step).toHaveAttribute("aria-current", "step");
    expect(api.callsTo("GET", "/api/sessions/sess_1?workspaceId=ws_1")).not.toHaveLength(0);
    // /sessions/:id is under Sessions in the nav, with a breadcrumb back
    const nav = screen.getByRole("menu", { name: "Main navigation" });
    expect(within(nav).getByRole("menuitem", { name: /Sessions/ })).toHaveClass(
      "ant-menu-item-selected",
    );
    await userEvent.click(screen.getByRole("link", { name: "Sessions" }));
    expect(await screen.findByRole("heading", { level: 2, name: "Sessions" })).toBeInTheDocument();
    await waitFor(() => expect(document.title).toMatch(/^Sessions/));
  }, 60_000);

  it("renders Monitors through the app", async () => {
    mockApi();
    renderAt(<AppRoutes />, "/monitors");
    expect(await screen.findByText("Duplicate tool call")).toBeInTheDocument();
  }, 60_000);
});
