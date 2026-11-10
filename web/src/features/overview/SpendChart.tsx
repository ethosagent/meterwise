import { Card, Skeleton } from "antd";
import { Suspense, lazy } from "react";
import type { SavingsDay } from "../../contract/generated";
import { formatUsd } from "../../lib/money";

// recharts is large and only this card needs it: load it when the card first renders.
const Impl = lazy(() => import("./SpendChartImpl"));

/** Spend over the range: paid (solid) against would-have-paid (dashed), the saved band shaded. */
export function SpendChart({ days }: { days: readonly SavingsDay[] }) {
  const first = days[0];
  const last = days[days.length - 1];
  const label =
    first && last
      ? `Spend by UTC day, ${first.day} to ${last.day}. Latest day paid ${formatUsd(last.paidUsd)}.`
      : "Spend by UTC day: no data.";
  return (
    <Card title="Spend" aria-label="Spend chart">
      <div role="img" aria-label={label}>
        <Suspense fallback={<Skeleton active />}>
          <Impl days={days} />
        </Suspense>
      </div>
    </Card>
  );
}
