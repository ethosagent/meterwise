import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemorySettings, MemoryStats } from "../../contract/generated";
import { MEMORY_SETTINGS, MEMORY_STATS } from "../../test/fixtures/memory";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { MemoryCard } from "./MemoryCard";

const SEL = { range: "7d", from: "", to: "" } as const;
const enabled = (mode: "off" | "observe" | "hint"): MemorySettings => ({
  config: { ...MEMORY_SETTINGS.config, mode },
  enabled: true,
  disabledReason: null,
});

function show(settings: MemorySettings, stats: MemoryStats = MEMORY_STATS) {
  const api = mockApi({ features: { memory: settings, memoryData: { stats } } });
  renderAt(<MemoryCard workspaceId="ws_1" sel={SEL} />);
  return api;
}

describe("Memory card", () => {
  beforeEach(() => window.localStorage.clear());

  it("shows the figures as the server gave them, the likely saving grey and apart", async () => {
    show(enabled("hint"));
    const card = within(await screen.findByLabelText("Memory figures"));
    expect(card.getByText("Traces stored")).toBeInTheDocument();
    expect(card.getByText("3")).toBeInTheDocument();
    expect(card.getByText("2")).toBeInTheDocument();
    expect(card.getByText("$0.13")).toBeInTheDocument();
    expect(card.getByText("likely")).toBeInTheDocument();
    expect(card.getByText("50%")).toBeInTheDocument();
    expect(screen.getByText(/not part of the saved figure/)).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/saved\s*\+\s*likely/i);
  });

  it("in observe it says memory is observing and how often it would have hinted", async () => {
    show(enabled("observe"), { ...MEMORY_STATS, mode: "observe", hits: 4, observedHits: 4 });
    expect(
      await screen.findByText(/memory is observing — would have hinted 4 times/),
    ).toBeInTheDocument();
    expect(await screen.findByText("3 traces learned, observing")).toBeInTheDocument();
  });

  it("with nothing learned yet it says memory starts after the first completed session", async () => {
    show(enabled("observe"), { ...MEMORY_STATS, mode: "observe", traces: 0, hintedSessions: 0 });
    expect(
      await screen.findByText("Memory starts learning after your first completed session"),
    ).toBeInTheDocument();
  });

  it.each([
    ["no_key", "Memory is not configured on this server"],
    ["privacy", "Memory needs capture on for this workspace; privacy mode keeps it off"],
    ["budget", "Paused: today's memory budget is used"],
  ])("says so when memory is disabled: %s", async (reason, text) => {
    show({ ...MEMORY_SETTINGS, disabledReason: reason });
    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.queryByLabelText("Memory figures")).not.toBeInTheDocument();
  });

  it("celebrates the first hint once, with a link to the remembered sessions", async () => {
    show(enabled("hint"));
    expect(
      await screen.findByText("Memory gave its first hint.", { exact: false }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "See the step" })).toHaveAttribute(
      "href",
      "/sessions?tag=remembered",
    );
    expect(window.localStorage.getItem("memory.firstHint.ws_1")).not.toBeNull();
  });

  it("does not celebrate again once it has", async () => {
    window.localStorage.setItem("memory.firstHint.ws_1", "1");
    show(enabled("hint"));
    await screen.findByLabelText("Memory figures");
    await waitFor(() => expect(screen.queryByText(/first hint/)).not.toBeInTheDocument());
  });
});
