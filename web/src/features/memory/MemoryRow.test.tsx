import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { MemorySettings } from "../../contract/generated";
import { MEMORY_SETTINGS } from "../../test/fixtures/memory";
import { MEMBER_SNAPSHOT } from "../../test/mockAccess";
import { MEMBER_ME, mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { MemoryRow } from "./MemoryRow";

const settings = (mode: "off" | "observe" | "hint" = "observe"): MemorySettings => ({
  config: { ...MEMORY_SETTINGS.config, mode, model: "gpt-mini", embedModel: "embed-s" },
  enabled: true,
  disabledReason: null,
});

function show(readOnly = false, init: Parameters<typeof mockApi>[0] = {}) {
  const api = mockApi({ features: { memory: settings() }, ...init });
  renderAt(<MemoryRow workspaceId="ws_1" readOnly={readOnly} canStats />);
  return api;
}

const patches = (api: ReturnType<typeof mockApi>) => api.callsTo("PATCH", /\/memory$/);

describe("Memory settings row", () => {
  it("sends exact partial bodies: mode, hints per session, models", async () => {
    const api = show();
    const user = userEvent.setup();
    await user.click(await screen.findByText("Off"));
    await waitFor(() => expect(patches(api)).toHaveLength(1));
    expect(patches(api)[0]?.body).toEqual({ mode: "off" });
    await user.click(screen.getByRole("combobox", { name: "Max hints per session" }));
    await user.click(await screen.findByText("3"));
    await waitFor(() => expect(patches(api)).toHaveLength(2));
    expect(patches(api)[1]?.body).toEqual({ maxHintsPerSession: 3 });
    const model = screen.getByRole("textbox", { name: "Memory model" });
    await user.clear(model);
    await user.type(model, "gpt-nano");
    await user.click(screen.getByRole("button", { name: "Save models" }));
    await waitFor(() => expect(patches(api)).toHaveLength(3));
    expect(patches(api)[2]?.body).toEqual({ model: "gpt-nano" });
  });

  it("sends the threshold when the slider settles", async () => {
    const api = show();
    const slider = await screen.findByRole("slider", { name: "Similarity threshold" });
    fireEvent.keyDown(slider, { key: "ArrowRight", code: "ArrowRight", keyCode: 39 });
    fireEvent.keyUp(slider, { key: "ArrowRight", code: "ArrowRight", keyCode: 39 });
    await waitFor(() => expect(patches(api)).toHaveLength(1));
    expect(patches(api)[0]?.body).toEqual({ thresholdBp: 8300 });
  });

  it("hint waits behind a confirm; cancel sends nothing", async () => {
    const api = show();
    const user = userEvent.setup();
    await user.click((await screen.findAllByText("Hint"))[0] as HTMLElement);
    expect(await screen.findByText("Turn on Memory hints?")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(patches(api)).toHaveLength(0);
    await user.click(screen.getAllByText("Hint")[0] as HTMLElement);
    await user.click(await screen.findByRole("button", { name: "Hint" }));
    await waitFor(() => expect(patches(api)).toHaveLength(1));
    expect(patches(api)[0]?.body).toEqual({ mode: "hint" });
  });

  it("Forget memory confirms, then deletes the traces", async () => {
    const api = show();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Forget memory" }));
    expect(await screen.findByText("Forget everything memory has learned?")).toBeInTheDocument();
    expect(api.callsTo("DELETE", /traces$/)).toHaveLength(0);
    const confirm = (await screen.findAllByRole("button", { name: "Forget memory" })).at(-1);
    if (!confirm) throw new Error("no confirm button");
    await user.click(confirm);
    await waitFor(() => expect(api.callsTo("DELETE", /traces$/)).toHaveLength(1));
    expect(api.features.memoryData.traces()).toEqual([]);
  });

  it("shows our own model cost this month", async () => {
    show();
    expect(await screen.findByText(/cost us \$0\.04 this month/)).toBeInTheDocument();
  });

  it("without monitor_config:write every control is disabled", async () => {
    show(true);
    await screen.findByRole("slider", { name: "Similarity threshold" });
    expect(screen.getByRole("combobox", { name: "Max hints per session" })).toBeDisabled();
    expect(screen.getByRole("textbox", { name: "Memory model" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save models" })).toBeDisabled();
    for (const r of screen.getAllByRole("radio")) expect(r).toBeDisabled();
  });

  it("hides Forget memory without workspace:manage", async () => {
    show(false, {
      me: MEMBER_ME,
      access: { snapshot: MEMBER_SNAPSHOT },
    });
    await screen.findByRole("slider", { name: "Similarity threshold" });
    expect(screen.queryByRole("button", { name: "Forget memory" })).not.toBeInTheDocument();
  });

  it("explains privacy mode in the row", async () => {
    mockApi({ features: { memory: { ...MEMORY_SETTINGS, disabledReason: "privacy" } } });
    renderAt(<MemoryRow workspaceId="ws_1" readOnly={false} canStats />);
    expect(
      await screen.findByText("privacy mode: off until capture is enabled"),
    ).toBeInTheDocument();
  });
});
