import { Input, Segmented, Space, Typography, theme } from "antd";
import { useState } from "react";
import { type RangeSel, utcToday, validCustomRange } from "../api/range";

const PRESETS = [
  { label: "7 days", value: "7d" },
  { label: "30 days", value: "30d" },
  { label: "Custom", value: "custom" },
] as const;

/**
 * The range picker: 7d, 30d or a custom UTC day span (at most 90 days, not after today). The
 * selection is lifted to the URL by the caller; a custom pair is applied only when the server would
 * accept it. `presets` limits the choice for views without a custom range.
 */
export function RangeControl({
  value,
  onChange,
  custom = true,
}: {
  value: RangeSel;
  onChange: (next: RangeSel) => void;
  custom?: boolean;
}) {
  const { token } = theme.useToken();
  const today = utcToday();
  const [from, setFrom] = useState(value.from);
  const [to, setTo] = useState(value.to);
  const [picking, setPicking] = useState(value.range === "custom");
  const apply = (f: string, t: string) => {
    if (validCustomRange(f, t, today)) onChange({ range: "custom", from: f, to: t });
  };
  const items = custom ? PRESETS : PRESETS.slice(0, 2);
  return (
    <Space wrap size={token.marginSM} align="center">
      <Segmented
        aria-label="Date range"
        options={[...items]}
        value={picking ? "custom" : value.range}
        onChange={(v) => {
          if (v === "custom") {
            setPicking(true);
            apply(from, to);
          } else {
            setPicking(false);
            onChange({ range: v === "30d" ? "30d" : "7d", from: "", to: "" });
          }
        }}
      />
      {picking && custom ? (
        <Space size={token.marginXS}>
          <Input
            type="date"
            aria-label="From"
            value={from}
            max={today}
            onChange={(e) => {
              setFrom(e.target.value);
              apply(e.target.value, to);
            }}
          />
          <Input
            type="date"
            aria-label="To"
            value={to}
            max={today}
            onChange={(e) => {
              setTo(e.target.value);
              apply(from, e.target.value);
            }}
          />
          {from && to && !validCustomRange(from, to, today) ? (
            <Typography.Text type="danger">
              Pick up to 90 UTC days, ending today at the latest.
            </Typography.Text>
          ) : null}
        </Space>
      ) : null}
      <Typography.Text type="secondary">Days are UTC.</Typography.Text>
    </Space>
  );
}
