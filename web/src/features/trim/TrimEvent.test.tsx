import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { SAVER_DETAIL } from "../../test/fixtures/sessions";
import { stubTrimRoutes, TRIM_PREVIEW, TRIM_STATS_ACTED } from "../../test/fixtures/trim";
import { mockApi } from "../../test/mockApi";
import { renderAt } from "../../test/render";
import { TrimEvent } from "./TrimEvent";

const acted = SAVER_DETAIL.calls.find((c) => c.trimAction === "acted");
const observed = SAVER_DETAIL.calls.find((c) => c.trimAction === "observe");

describe("TrimEvent", () => {
  it("says how many tokens were trimmed and how sure the saving is", () => {
    mockApi();
    if (!acted || !observed) throw new Error("fixture lacks an acted and an observed call");
    renderAt(<TrimEvent call={acted} />);
    expect(screen.getByText("Trimmed")).toBeInTheDocument();
    expect(screen.getByText(/^−[\d,]+ tokens$/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Details" })).not.toBeInTheDocument();
  });

  it("an observed call is a would-trim, never a saving", () => {
    mockApi();
    if (!observed) throw new Error("fixture lacks an observed call");
    renderAt(<TrimEvent call={observed} />);
    expect(screen.getByText("Would trim")).toBeInTheDocument();
    expect(screen.getByText("would save")).toBeInTheDocument();
  });

  it("Details opens the per-stage sizes and fixed markers, and no text", async () => {
    mockApi();
    const seen = stubTrimRoutes(TRIM_STATS_ACTED, TRIM_PREVIEW);
    if (!acted) throw new Error("fixture lacks an acted call");
    renderAt(<TrimEvent call={acted} workspaceId="ws_1" />);
    await userEvent.setup().click(screen.getByRole("button", { name: "Details" }));
    const drawer = within(await screen.findByRole("dialog"));
    expect(await drawer.findByText("Duplicate lines")).toBeInTheDocument();
    expect(drawer.getByText("Long logs and diffs")).toBeInTheDocument();
    expect(drawer.getByText("5,800 → 2,400")).toBeInTheDocument();
    expect(drawer.getByText("line (×N)")).toBeInTheDocument();
    expect(drawer.getByText("… N lines folded; K errors kept …")).toBeInTheDocument();
    expect(drawer.getByText(/No request text is stored/)).toBeInTheDocument();
    expect(seen.some((p) => p.endsWith(`/api/workspaces/ws_1/calls/${acted.id}/trim`))).toBe(true);
  });
});
