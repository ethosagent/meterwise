import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DAYS_7 } from "../../test/fixtures/savings";
import { SpendChart } from "./SpendChart";

// The shared test setup stubs the chart module so full-app tests do not pay for recharts; this file
// is the one place that loads the real one.
vi.unmock("./SpendChartImpl");

describe("SpendChart", () => {
  it("labels the region and loads the lazy chart module into it", async () => {
    render(<SpendChart days={DAYS_7} />);
    const region = screen.getByRole("img", {
      name: /Spend by UTC day, 2026-11-04 to 2026-11-10/,
    });
    await waitFor(() =>
      expect(region.querySelector(".recharts-responsive-container")).not.toBeNull(),
    );
    expect(region.querySelector(".ant-skeleton")).toBeNull();
  });

  it("says there is no data for an empty range", () => {
    render(<SpendChart days={[]} />);
    expect(screen.getByRole("img", { name: "Spend by UTC day: no data." })).toBeInTheDocument();
  });
});
