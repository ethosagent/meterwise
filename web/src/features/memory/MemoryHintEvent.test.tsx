import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { MEMORY_HIT, SAVER_DETAIL, SAVER_SESSION } from "../../test/fixtures/sessions";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { SessionDetailPage } from "../sessions/SessionDetailPage";
import { MemoryHintEvent } from "./MemoryHintEvent";

function page() {
  const api = mockApi({ savings: { sessions: [SAVER_SESSION], details: [SAVER_DETAIL] } });
  renderAt(
    <Routes>
      <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
    </Routes>,
    `/sessions/${SAVER_SESSION.id}`,
  );
  return api;
}

describe("Remembered card", () => {
  it("shows the exact hint, the similarity, the source session and the likely saving", async () => {
    page();
    const step = within(await screen.findByTestId("step-call_16"));
    expect(within(step.getByLabelText("Remembered")).getByText("Remembered")).toBeInTheDocument();
    expect(step.getByText(MEMORY_HIT.hint)).toBeInTheDocument();
    expect(step.getByText("87% similar")).toBeInTheDocument();
    expect(step.getByRole("link", { name: "source session" })).toHaveAttribute(
      "href",
      "/sessions/sess_1",
    );
    expect(
      step.getByText(/likely saved \$0\.05 compared with similar sessions/),
    ).toBeInTheDocument();
  });

  it("posts the feedback and refetches the session so the stored value shows", async () => {
    const api = page();
    const user = userEvent.setup();
    const step = within(await screen.findByTestId("step-call_16"));
    const before = api.callsTo("GET", /^\/api\/sessions\//).length;
    await user.click(step.getByRole("button", { name: "Hint helped" }));
    await waitFor(() =>
      expect(api.callsTo("POST", "/api/workspaces/ws_1/memory-hits/hit_1/feedback")).toHaveLength(
        1,
      ),
    );
    expect(api.callsTo("POST", /memory-hits/)[0]?.body).toEqual({ feedback: "helpful" });
    await waitFor(() =>
      expect(api.callsTo("GET", /^\/api\/sessions\//).length).toBeGreaterThan(before),
    );
    expect(api.features.memoryData.feedbackOf("hit_1")).toBe("helpful");
  });

  it("a pressed button clears the feedback with null", async () => {
    const api = mockApi();
    const hit = { ...MEMORY_HIT, feedback: "irrelevant" };
    renderAt(<MemoryHintEvent hit={hit} workspaceId="ws_1" sessionId="sess_7" />);
    const user = userEvent.setup();
    const button = screen.getByRole("button", { name: "Hint was irrelevant" });
    expect(button).toHaveAttribute("aria-pressed", "true");
    await user.click(button);
    await waitFor(() => expect(api.callsTo("POST", /memory-hits/)).toHaveLength(1));
    expect(api.callsTo("POST", /memory-hits/)[0]?.body).toEqual({ feedback: null });
  });

  it("an observe-mode hit reads would have hinted", () => {
    mockApi();
    const hit = { ...MEMORY_HIT, mode: "observe" as const, injected: false };
    renderAt(<MemoryHintEvent hit={hit} workspaceId="ws_1" sessionId="sess_7" />);
    expect(screen.getByText("Would have hinted")).toBeInTheDocument();
    expect(screen.getByText(/would likely save \$0\.05/)).toBeInTheDocument();
    expect(screen.queryByText("Remembered")).not.toBeInTheDocument();
  });
});
