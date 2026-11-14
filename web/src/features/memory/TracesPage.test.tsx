import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { MemorySettings } from "../../contract/generated";
import { MEMORY_SETTINGS } from "../../test/fixtures/memory";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { TracesPage } from "./TracesPage";

const enabled = (over: Partial<MemorySettings["config"]> = {}): MemorySettings => ({
  config: { ...MEMORY_SETTINGS.config, mode: "hint", ...over },
  enabled: true,
  disabledReason: null,
});

describe("Traces browser", () => {
  it("lists the traces, pages with Load more and opens one in a drawer", async () => {
    const api = mockApi({ features: { memory: enabled() } });
    renderAt(<TracesPage />, "/settings/memory");
    expect(await screen.findByText("Fix build 1: missing go.sum entry")).toBeInTheDocument();
    expect(screen.queryByText("Fix build 3: missing go.sum entry")).not.toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByText("Fix build 3: missing go.sum entry")).toBeInTheDocument();
    expect(api.callsTo("GET", /traces\?.*cursor=2/)).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Fix build 1: missing go.sum entry" }));
    const drawer = within(await screen.findByRole("dialog"));
    expect(await drawer.findByText("Reused 2 times")).toBeInTheDocument();
    expect(drawer.getByText("go mod tidy")).toBeInTheDocument();
    expect(drawer.getByRole("link", { name: "sess_7" })).toHaveAttribute(
      "href",
      "/sessions/sess_7",
    );
    expect(drawer.getByRole("link", { name: "sess_9" })).toBeInTheDocument();
  });

  it("without session:read shows no access and asks for no traces", async () => {
    const api = mockApi({
      me: MEMBER_ME,
      access: {
        snapshot: {
          admin: false,
          grants: MEMBER_SNAPSHOT.grants.filter((g) => g.resourceType !== "session"),
        },
      },
    });
    renderAt(<TracesPage />, "/settings/memory");
    expect(
      await screen.findByText("You don't have access to remembered solutions"),
    ).toBeInTheDocument();
    expect(api.callsTo("GET", /traces/)).toEqual([]);
  });

  it.each([
    ["no_key", "Memory is not configured on this server"],
    ["privacy", "Memory needs capture on for this workspace; privacy mode keeps it off"],
    ["budget", "Paused: today's memory budget is used"],
  ])("says why memory is not running: %s", async (reason, text) => {
    mockApi({
      features: {
        memory: { ...MEMORY_SETTINGS, disabledReason: reason },
        memoryData: { traces: [] },
      },
    });
    renderAt(<TracesPage />, "/settings/memory");
    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.getByText("No remembered solutions")).toBeInTheDocument();
  });

  it("an enabled workspace with no traces says memory starts learning after the first completed session", async () => {
    mockApi({ features: { memory: enabled({ mode: "observe" }), memoryData: { traces: [] } } });
    renderAt(<TracesPage />, "/settings/memory");
    expect(
      await screen.findByText("Memory starts learning after your first completed session"),
    ).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("button", { name: "Load more" })).toBeNull());
  });
});
