import { describe, expect, it } from "vitest";
import type { SavingsDay } from "../../contract/generated";
import { DAYS_7 } from "../../test/fixtures/savings";
import { chartSeries } from "./chartSeries";

const day = (over: Partial<SavingsDay>): SavingsDay => ({
  day: "2026-11-01",
  paidUsd: "0.000000",
  savedUsd: "0.000000",
  likelySavedUsd: "0.000000",
  wouldHavePaidUsd: "0.000000",
  calls: 0,
  sessions: 0,
  ...over,
});

describe("chartSeries", () => {
  it("passes each day's fields through as the server sent them, with no arithmetic", () => {
    // wouldHavePaid deliberately is NOT paid + saved: a client that recomputed it would show 3, not 7.
    const d = day({ paidUsd: "1.000000", savedUsd: "2.000000", wouldHavePaidUsd: "7.000000" });
    const [p] = chartSeries([d]);
    expect(p).toMatchObject({ day: "2026-11-01", paid: 1, saved: 2, wouldHavePaid: 7 });
    expect(p?.raw).toBe(d);
  });

  it("never folds likely saved into any series", () => {
    const [p] = chartSeries([
      day({ paidUsd: "1.000000", savedUsd: "1.000000", likelySavedUsd: "0.500000" }),
    ]);
    expect(p?.saved).toBe(1);
    expect(Object.values(p ?? {}).includes(1.5)).toBe(false);
  });

  it("keeps every day in order, the zero-filled ones too", () => {
    const pts = chartSeries(DAYS_7);
    expect(pts.map((x) => x.day)).toEqual(DAYS_7.map((x) => x.day));
    expect(pts[0]).toMatchObject({ paid: 0, saved: 0, wouldHavePaid: 0 });
  });
});
