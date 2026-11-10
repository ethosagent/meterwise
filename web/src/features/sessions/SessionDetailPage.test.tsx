import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { PermissionSnapshot, SessionDetail } from "../../contract/generated";
import { DETAIL, EVENT_BLOCK } from "../../test/fixtures/sessions";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { SessionDetailPage } from "./SessionDetailPage";

function at(path: string) {
  return renderAt(
    <Routes>
      <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
    </Routes>,
    path,
  );
}
const NO_SESSIONS: PermissionSnapshot = {
  admin: false,
  grants: MEMBER_SNAPSHOT.grants.filter((g) => g.resourceType !== "session"),
};
const card = (id: string) => screen.getByTestId(`evidence-${id}`);

describe("SessionDetailPage", () => {
  it("shows the header with spent, saved and likely as three separate numbers", async () => {
    mockApi();
    at("/sessions/sess_1");
    const header = await screen.findByLabelText("Session header");
    expect(header).toHaveTextContent("opencode");
    expect(header).toHaveTextContent("gpt-4o");
    expect(header).toHaveTextContent("12m 0s");
    expect(header).toHaveTextContent("Spent$0.80");
    expect(header).toHaveTextContent("Saved$0.25");
    expect(header).toHaveTextContent("Likely saved$0.10");
    expect(document.body.textContent).not.toContain("$0.35");
    expect(screen.getByRole("link", { name: "Sessions" })).toHaveAttribute("href", "/sessions");
  });

  it("?event= highlights the step of that event and shows its stored evidence", async () => {
    mockApi();
    at("/sessions/sess_1?event=ev_1");
    const step = await screen.findByTestId("step-call_3");
    expect(step).toHaveAttribute("aria-current", "step");
    expect(screen.getByTestId("step-call_4")).not.toHaveAttribute("aria-current");
    expect(card("ev_1")).toHaveAttribute("aria-current", "true");
    expect(card("ev_2")).not.toHaveAttribute("aria-current");
    const ev = within(card("ev_1"));
    expect(ev.getByText("Duplicate tool call")).toBeInTheDocument();
    expect(ev.getByText(/Tool read_file/)).toBeInTheDocument();
    expect(ev.getByText(/Path src\/main\.go/)).toBeInTheDocument();
    expect(ev.getByText(/Seen 3 times \(threshold 2\)/)).toBeInTheDocument();
    expect(ev.getByText(/Saved \$0\.25 \(30,000 tokens\)/)).toBeInTheDocument();
  });

  it("?call= highlights that step by call id", async () => {
    mockApi();
    at("/sessions/sess_1?call=call_4");
    expect(await screen.findByTestId("step-call_4")).toHaveAttribute("aria-current", "step");
    expect(screen.getByTestId("step-call_3")).not.toHaveAttribute("aria-current");
    expect(card("ev_2")).toHaveAttribute("aria-current", "true");
  });

  it("an unknown event or call highlights nothing", async () => {
    mockApi();
    at("/sessions/sess_1?event=nope&call=nope");
    await screen.findByTestId("step-call_3");
    expect(document.querySelector('[aria-current="step"]')).toBeNull();
  });

  it("renders a blocked step as 'saved $x' with the answered-by-the-proxy note, not a cached result", async () => {
    mockApi();
    at("/sessions/sess_1");
    const step = await screen.findByTestId("step-call_3");
    expect(within(step).getByText("saved $0.25")).toBeInTheDocument();
    expect(within(step).getByText("blocked")).toBeInTheDocument();
    expect(
      within(step).getByText("Answered by the proxy: pointed the agent back to call #2's result."),
    ).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/cached result/i);
    // the matched step links by call id
    expect(within(card("ev_1")).getByRole("link", { name: "call #2" })).toHaveAttribute(
      "href",
      "/sessions/sess_1?call=call_2",
    );
  });

  it("shows an evidence index of 0 as 'earlier call' and does not link it", async () => {
    mockApi();
    at("/sessions/sess_1");
    await screen.findByTestId("step-call_4");
    const note = within(card("ev_2"));
    expect(note.getByText(/Matched/)).toHaveTextContent("Matched earlier call");
    expect(note.queryByRole("link")).toBeNull();
    expect(note.getByText("Path hash ab12cd34")).toBeInTheDocument();
    expect(note.getByText(/Likely saved \$0\.10/)).toBeInTheDocument();
    // a note's step shows how many notes it carried
    expect(within(screen.getByTestId("step-call_4")).getByText("1 note added")).toBeInTheDocument();
  });

  it("words a block that points before the session as 'an earlier call's result'", async () => {
    const detail: SessionDetail = {
      ...DETAIL,
      monitorEvents: [{ ...EVENT_BLOCK, evidence: { ...EVENT_BLOCK.evidence, matchedCalls: [0] } }],
    };
    mockApi({ savings: { details: [detail] } });
    at("/sessions/sess_1");
    await screen.findByTestId("step-call_3");
    expect(
      screen.getByText(/pointed the agent back to an earlier call's result\./),
    ).toBeInTheDocument();
    expect(within(card("ev_1")).queryByRole("link")).toBeNull();
  });

  it("feedback round-trips: POST, refetch, toggle, clear", async () => {
    const api = mockApi();
    at("/sessions/sess_1");
    await screen.findByTestId("step-call_3");
    const user = userEvent.setup();
    const wrongBtn = () => within(card("ev_1")).getByRole("button", { name: "Mark wrong" });
    const detailGets = () => api.callsTo("GET", /\/api\/sessions\/sess_1\?/).length;
    expect(wrongBtn()).toHaveAttribute("aria-pressed", "false");
    const before = detailGets();
    await user.click(wrongBtn());
    await waitFor(() => expect(wrongBtn()).toHaveAttribute("aria-pressed", "true"));
    expect(api.callsTo("POST", /monitor-events\/ev_1\/feedback$/)[0]?.body).toEqual({
      feedback: "wrong",
    });
    expect(detailGets()).toBeGreaterThan(before); // the detail was invalidated and refetched
    expect(api.savings.feedbackOf("ev_1")).toBe("wrong");
    await user.click(wrongBtn());
    await waitFor(() => expect(wrongBtn()).toHaveAttribute("aria-pressed", "false"));
    expect(api.callsTo("POST", /ev_1\/feedback$/)[1]?.body).toEqual({ feedback: null });
    // ev_2 starts as wrong; marking it correct replaces it
    const correct2 = within(card("ev_2")).getByRole("button", { name: "Mark correct" });
    expect(within(card("ev_2")).getByRole("button", { name: "Mark wrong" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(correct2);
    await waitFor(() => expect(api.savings.feedbackOf("ev_2")).toBe("correct"));
    await waitFor(() => expect(correct2).toHaveAttribute("aria-pressed", "true"));
  });

  it("explains the cost in the rail: tokens per call, repeats, and a copyable suggestion", async () => {
    mockApi();
    const user = userEvent.setup();
    at("/sessions/sess_1");
    expect(await screen.findByText("Why this session cost what it did")).toBeInTheDocument();
    const rail = screen.getByLabelText("Input tokens per call");
    expect(within(rail).getByText("14,000")).toBeInTheDocument();
    expect(screen.getByTestId("ctx-call_4").style.width).toBe("100%");
    expect(screen.getByText("read_file read src/main.go 4 times.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Copy suggestion" }));
    expect(await navigator.clipboard.readText()).toBe(DETAIL.insight.suggestion);
  });

  it("says when the session was truncated", async () => {
    mockApi({ savings: { details: [{ ...DETAIL, truncated: true }] } });
    at("/sessions/sess_1");
    expect(await screen.findByText(/more calls than are shown/)).toBeInTheDocument();
  });

  it("a session that does not exist is an error, not a blank page", async () => {
    mockApi();
    at("/sessions/missing");
    expect(await screen.findByText("Could not load this session.")).toBeInTheDocument();
  });

  it("without session:read shows no access and reads no session", async () => {
    const api = mockApi({ me: MEMBER_ME, access: { snapshot: NO_SESSIONS } });
    at("/sessions/sess_1");
    expect(await screen.findByText("You don't have access to sessions")).toBeInTheDocument();
    expect(api.callsTo("GET", /\/api\/sessions\//)).toEqual([]);
  });
});
