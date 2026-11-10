import type { SavingsDay } from "../../contract/generated";
import { microToNumber } from "../../lib/money";

/** One chart point. Every value is a day's field as the server sent it: no sum, difference or fill is computed here. */
export interface ChartPoint {
  day: string;
  paid: number;
  saved: number;
  wouldHavePaid: number;
  raw: SavingsDay;
}

/**
 * The series for the spend chart. `paid` (solid) and `saved` (the shaded band) are stacked by the chart
 * itself; `wouldHavePaid` (dashed) is the server's own field, never recomputed. Likely saved is not drawn.
 */
export function chartSeries(days: readonly SavingsDay[]): ChartPoint[] {
  return days.map((d) => ({
    day: d.day,
    paid: microToNumber(d.paidUsd),
    saved: microToNumber(d.savedUsd),
    wouldHavePaid: microToNumber(d.wouldHavePaidUsd),
    raw: d,
  }));
}
