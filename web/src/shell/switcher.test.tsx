import { useQuery } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { workspaceScoped } from "../api/identity";
import { AppRoutes } from "../routes";
import { mockApi } from "../test/mockApi";
import { renderAt } from "../test/render";

let hits = 0;

/** A stand-in for any page's data: keyed under the workspace scope, counting how often it is fetched. */
function Probe() {
  const q = useQuery({
    queryKey: workspaceScoped("overview", "totals"),
    queryFn: async () => ++hits,
  });
  return <output data-testid="probe">{q.data}</output>;
}

describe("workspace switcher", () => {
  it("re-scopes: refetches workspace-scoped queries, leaves org-level ones alone", async () => {
    hits = 0;
    const api = mockApi();
    renderAt(
      <>
        <Probe />
        <AppRoutes />
      </>,
      "/overview",
    );
    const sw = await screen.findByTestId("workspace-switcher");
    await waitFor(() => expect(sw).toHaveTextContent("Default workspace"));
    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("1"));
    const listCalls = api.callsTo("GET", "/api/workspaces").length;

    await userEvent.click(sw);
    await userEvent.click(await screen.findByRole("menuitem", { name: "Research" }));

    await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("2"));
    await waitFor(() => expect(sw).toHaveTextContent("Research"));
    expect(api.callsTo("POST", "/api/workspaces/ws_3/switch")).toHaveLength(1);
    expect(api.callsTo("GET", "/api/workspaces")).toHaveLength(listCalls);
  });

  it("does not call the API when the already-active workspace is picked", async () => {
    const api = mockApi();
    renderAt(<AppRoutes />, "/overview");
    const sw = await screen.findByTestId("workspace-switcher");
    await waitFor(() => expect(sw).toHaveTextContent("Default workspace"));
    await userEvent.click(sw);
    await userEvent.click(await screen.findByRole("menuitem", { name: "Default workspace" }));
    expect(api.callsTo("POST", /switch$/)).toHaveLength(0);
  });
});
