import { screen, within } from "@testing-library/react";
import { Route, Routes } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { SAVER_DETAIL, SAVER_SESSION, SESSIONS } from "../../test/fixtures/sessions";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { SessionsPage } from "./Page";
import { SessionDetailPage } from "./SessionDetailPage";

describe("saver events on a session", () => {
  it("renders trim, route and memory cards on their steps with certain and would-save apart", async () => {
    mockApi({ savings: { sessions: [SAVER_SESSION], details: [SAVER_DETAIL] } });
    renderAt(
      <Routes>
        <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
      </Routes>,
      `/sessions/${SAVER_SESSION.id}`,
    );
    const s11 = within(await screen.findByTestId("step-call_11"));
    expect(s11.getByText("Trimmed −1,000 tokens")).toBeInTheDocument();
    expect(s11.getByText("certain")).toBeInTheDocument();
    expect(within(screen.getByTestId("step-call_12")).getByText("likely")).toBeInTheDocument();
    const s13 = within(screen.getByTestId("step-call_13"));
    expect(s13.getByText("Would trim")).toBeInTheDocument();
    expect(s13.getByText("would save")).toBeInTheDocument();
    const s14 = within(screen.getByTestId("step-call_14"));
    expect(s14.getByText("Routed")).toBeInTheDocument();
    expect(s14.getByText(/^saved \$\d+\.\d+ vs gpt-4\.1$/)).toBeInTheDocument();
    expect(within(screen.getByTestId("step-call_15")).getByText("Would route")).toBeInTheDocument();
    const s16 = within(screen.getByTestId("step-call_16"));
    expect(s16.getByText(/go mod tidy/)).toBeInTheDocument();
    expect(s16.getByRole("link", { name: "source session" })).toHaveAttribute(
      "href",
      "/sessions/sess_1",
    );
  });

  it("the sessions list shows a Tags column and filters by tag", async () => {
    const api = mockApi({ savings: { sessions: [...SESSIONS, SAVER_SESSION] } });
    renderAt(<SessionsPage />, "/sessions?tag=trimmed");
    const row = (await screen.findByText(/2026-11-09 10:07:00/)).closest("tr") as HTMLElement;
    expect(within(row).getByText("Trimmed")).toBeInTheDocument();
    expect(within(row).getByText("Routed")).toBeInTheDocument();
    expect(within(row).getByText("Remembered")).toBeInTheDocument();
    expect(screen.getAllByRole("row")).toHaveLength(2); // header + the one trimmed session
    expect(api.callsTo("GET", /sessions\?.*tag=trimmed/)).not.toHaveLength(0);
  });
});
