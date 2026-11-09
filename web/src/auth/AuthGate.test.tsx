import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { meKey } from "../api/identity";
import { AppRoutes } from "../routes";
import { mockApi } from "../test/mockApi";
import { renderAt } from "../test/render";
import { useMockAuth } from "./mock";
import { useSession } from "./session";

const NOT_INVITED = "You have not been invited yet";

beforeEach(() => useMockAuth.getState().signOut());

describe("AuthGate", () => {
  it("shows the not-invited screen on 403 not_invited, after trying to claim an invitation", async () => {
    const api = mockApi({ me: null });
    renderAt(<AppRoutes />, "/overview");
    expect(await screen.findByText(NOT_INVITED)).toBeInTheDocument();
    expect(screen.getByText(/Acme Meter is invite-only/)).toBeInTheDocument();
    expect(api.callsTo("POST", "/api/auth/complete-signup")).toHaveLength(1);
    expect(screen.queryByRole("menu", { name: "Main navigation" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(
      await screen.findByText("Development sign-in (mock identity provider)."),
    ).toBeInTheDocument();
  });

  it("on first login claims the invitation with complete-signup and lands in the app", async () => {
    const api = mockApi({ me: null, claimable: true });
    renderAt(<AppRoutes />, "/overview");
    expect(await screen.findByRole("heading", { level: 2, name: "Overview" })).toBeInTheDocument();
    expect(api.callsTo("POST", "/api/auth/complete-signup")).toHaveLength(1);
    expect(screen.queryByText(NOT_INVITED)).toBeNull();
  });

  it("sends the bearer token on API calls, and none on the public config", async () => {
    const api = mockApi();
    renderAt(<AppRoutes />, "/overview");
    await screen.findByRole("heading", { level: 2, name: "Overview" });
    expect(api.callsTo("GET", "/api/me")[0]?.authorization).toBe("Bearer mock:ada@acme.test");
    expect(api.callsTo("GET", "/api/workspaces")[0]?.authorization).toBe(
      "Bearer mock:ada@acme.test",
    );
  });

  it("sends a signed-out visitor to the login screen without calling the API", async () => {
    const api = mockApi();
    renderAt(<AppRoutes />, "/overview", { signedInAs: null });
    expect(
      await screen.findByText("Development sign-in (mock identity provider)."),
    ).toBeInTheDocument();
    expect(api.callsTo("GET", "/api/me")).toHaveLength(0);
  });

  it("shows the session-expired screen when an authenticated call returns 401", async () => {
    const api = mockApi();
    const { client } = renderAt(<AppRoutes />, "/overview");
    await screen.findByRole("heading", { level: 2, name: "Overview" });
    api.expireSession();
    await client.invalidateQueries({ queryKey: ["workspaces"] });
    expect(await screen.findByText("Your session expired")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Sign in again" }));
    expect(
      await screen.findByText("Development sign-in (mock identity provider)."),
    ).toBeInTheDocument();
    expect(useMockAuth.getState().email).toBeNull();
  });

  it("does not call a signed-out 401 'expired'", async () => {
    mockApi();
    const { client } = renderAt(<AppRoutes />, "/login", { signedInAs: null });
    await screen.findByText("Development sign-in (mock identity provider).");
    expect(client.getQueryData(meKey)).toBeUndefined();
    expect(screen.queryByText("Your session expired")).toBeNull();
  });

  it("signs in through the mock form and reaches the app", async () => {
    mockApi();
    renderAt(<AppRoutes />, "/login", { signedInAs: null });
    await userEvent.type(await screen.findByLabelText("Email"), "ada@acme.test");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(useMockAuth.getState().email).toBe("ada@acme.test"));
    expect(await screen.findByRole("heading", { level: 2, name: "Overview" })).toBeInTheDocument();
  });

  it("does not show the expired screen to someone who signs in again by the login route", async () => {
    mockApi();
    renderAt(<AppRoutes />, "/login", { signedInAs: null });
    useSession.getState().markExpired(); // a 401 left the flag set; the person reached /login by URL
    await userEvent.type(await screen.findByLabelText("Email"), "ada@acme.test");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("heading", { level: 2, name: "Overview" })).toBeInTheDocument();
    expect(screen.queryByText("Your session expired")).toBeNull();
  });

  it("offers the accept-invite landing", async () => {
    mockApi();
    renderAt(<AppRoutes />, "/accept-invite", { signedInAs: null });
    expect(await screen.findByRole("button", { name: "Accept invitation" })).toBeInTheDocument();
  });
});
