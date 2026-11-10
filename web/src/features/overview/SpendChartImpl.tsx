import { Typography, theme } from "antd";
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { SavingsDay } from "../../contract/generated";
import { formatUsd } from "../../lib/money";
import { type ChartPoint, chartSeries } from "./chartSeries";

function DayTip({ active, payload }: { active?: boolean; payload?: { payload: ChartPoint }[] }) {
  const { token } = theme.useToken();
  const day: SavingsDay | undefined = payload?.[0]?.payload.raw;
  if (!active || !day) return null;
  return (
    <div
      style={{
        background: token.colorBgElevated,
        border: `${token.lineWidth}px ${token.lineType} ${token.colorBorderSecondary}`,
        borderRadius: token.borderRadius,
        padding: token.paddingSM,
      }}
    >
      <Typography.Text strong>{day.day}</Typography.Text>
      <div>Would have paid {formatUsd(day.wouldHavePaidUsd)}</div>
      <div>Paid {formatUsd(day.paidUsd)}</div>
      <div>Saved {formatUsd(day.savedUsd)}</div>
    </div>
  );
}

/**
 * Dashed would-have-paid, solid paid, the saved band shaded between them. Colours come from antd
 * tokens so light and dark both read. This module is the only importer of recharts and is lazy-loaded.
 */
export default function SpendChartImpl({ days }: { days: readonly SavingsDay[] }) {
  const { token } = theme.useToken();
  const data = chartSeries(days);
  return (
    <ResponsiveContainer width="100%" height={token.controlHeightLG * 7}>
      <ComposedChart data={data}>
        <CartesianGrid stroke={token.colorBorderSecondary} vertical={false} />
        <XAxis dataKey="day" stroke={token.colorTextSecondary} />
        <YAxis stroke={token.colorTextSecondary} tickFormatter={(v: number) => `$${v}`} />
        <Tooltip content={<DayTip />} />
        <Legend />
        <Area
          name="Paid"
          dataKey="paid"
          stackId="spend"
          stroke={token.colorText}
          strokeWidth={token.lineWidthBold}
          fill="none"
          isAnimationActive={false}
        />
        <Area
          name="Saved"
          dataKey="saved"
          stackId="spend"
          stroke="none"
          fill={token.colorSuccess}
          fillOpacity={0.25}
          isAnimationActive={false}
        />
        <Line
          name="Would have paid"
          dataKey="wouldHavePaid"
          stroke={token.colorTextTertiary}
          strokeWidth={token.lineWidthBold}
          strokeDasharray={`${token.sizeXS} ${token.sizeXXS}`}
          dot={false}
          isAnimationActive={false}
        />
      </ComposedChart>
    </ResponsiveContainer>
  );
}
