import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { SAVER_SESSION, SESSIONS } from "../../test/fixtures/sessions";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { SessionsPage } from "../sessions/Page";

describe("Remembered filter on the sessions list", () => {
  it("reads tag=remembered from the URL and asks the server for it", async () => {
    const api = mockApi({ savings: { sessions: [...SESSIONS, SAVER_SESSION] } });
    renderAt(<SessionsPage />, "/sessions?tag=remembered");
    const row = (await screen.findByText(/2026-11-09 10:07:00/)).closest("tr") as HTMLElement;
    expect(within(row).getByText("Remembered")).toBeInTheDocument();
    expect(screen.getAllByRole("row")).toHaveLength(2);
    expect(api.callsTo("GET", /sessions\?.*tag=remembered/)).not.toHaveLength(0);
  });

  it("choosing Saver > remembered puts the tag in the URL and filters the list", async () => {
    const api = mockApi({ savings: { sessions: [...SESSIONS, SAVER_SESSION] } });
    renderAt(<SessionsPage />, "/sessions");
    await screen.findByText(/2026-11-09 10:07:00/);
    const user = userEvent.setup();
    await user.click(screen.getByRole("combobox", { name: "Saver" }));
    await user.click(await screen.findByText("remembered"));
    await screen.findByText(/2026-11-09 10:07:00/);
    expect(api.callsTo("GET", /sessions\?.*tag=remembered/)).not.toHaveLength(0);
    expect(screen.getAllByRole("row")).toHaveLength(2);
  });
});
