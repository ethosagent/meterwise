import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { MEMORY_SETTINGS } from "../../test/fixtures/memory";
import { MEMORY_HIT, SAVER_SESSION, SESSIONS } from "../../test/fixtures/sessions";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { useTheme } from "../../theme/useTheme";
import { MemoryCard } from "../overview/MemoryCard";
import { SessionsPage } from "../sessions/Page";
import { MemoryHintEvent } from "./MemoryHintEvent";
import { MemoryRow } from "./MemoryRow";
import { TracesPage } from "./TracesPage";

const enabledHint = {
  config: { ...MEMORY_SETTINGS.config, mode: "hint" as const },
  enabled: true,
  disabledReason: null,
};

describe.each(["light", "dark"] as const)("memory surfaces in %s mode", (mode) => {
  beforeEach(() => {
    window.localStorage.clear();
    useTheme.setState({ mode });
  });

  it("renders the card, the settings row, the hint card and the traces browser", async () => {
    mockApi({ features: { memory: enabledHint } });
    renderAt(
      <>
        <MemoryCard workspaceId="ws_1" sel={{ range: "7d", from: "", to: "" }} />
        <MemoryRow workspaceId="ws_1" readOnly={false} canStats />
        <MemoryHintEvent hit={MEMORY_HIT} workspaceId="ws_1" sessionId="sess_7" />
        <TracesPage />
      </>,
      "/settings/memory",
    );
    expect(await screen.findByLabelText("Memory figures")).toBeInTheDocument();
    expect(await screen.findByLabelText("Memory settings")).toBeInTheDocument();
    expect(screen.getByLabelText("Remembered")).toBeInTheDocument();
    expect(await screen.findByText("Fix build 1: missing go.sum entry")).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe(mode);
  });

  it("renders the trace drawer and the sessions list with the Remembered tag", async () => {
    mockApi({
      features: { memory: enabledHint },
      savings: { sessions: [...SESSIONS, SAVER_SESSION] },
    });
    renderAt(<TracesPage />, "/settings/memory");
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: "Fix build 1: missing go.sum entry" }));
    expect(
      await within(await screen.findByRole("dialog")).findByText("Reused 2 times"),
    ).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe(mode);
  });

  it("renders the sessions list with the Remembered tag", async () => {
    mockApi({ savings: { sessions: [...SESSIONS, SAVER_SESSION] } });
    renderAt(<SessionsPage />, "/sessions?tag=remembered");
    const row = (await screen.findByText(/2026-11-09 10:07:00/)).closest("tr") as HTMLElement;
    expect(within(row).getByText("Remembered")).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe(mode);
  });
});
