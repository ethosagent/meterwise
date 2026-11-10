import { Popconfirm, Segmented, Tooltip } from "antd";
import { useState } from "react";
import {
  isMode,
  MODE_LABEL,
  type MonitorKey,
  type MonitorMode,
  monitorLabel,
  OBSERVE_ONLY,
  OBSERVE_ONLY_HINT,
} from "./monitorMeta";

const MODES: MonitorMode[] = ["observe", "note", "block"];

/**
 * The per-monitor mode: Observe / Nudge / Block. Block waits behind a confirm because it answers the call
 * itself. reasoning_loop and second_guessing offer Observe only (Nudge and Block disabled with a tooltip);
 * the API still accepts every mode. `readOnly` (no monitor_config:write) disables the whole control.
 */
export function ModeControl({
  monitor,
  mode,
  readOnly,
  busy,
  onChange,
}: {
  monitor: MonitorKey;
  mode: string;
  readOnly: boolean;
  busy: boolean;
  onChange: (next: MonitorMode) => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const observeOnly = OBSERVE_ONLY.has(monitor);
  const label = monitorLabel(monitor);
  const current: MonitorMode = isMode(mode) ? mode : "observe";
  return (
    <Popconfirm
      open={confirming}
      title={`Turn on Block for ${label}?`}
      description="Block answers the repeated call itself instead of sending it to the provider."
      okText="Block"
      cancelText="Cancel"
      onConfirm={() => {
        setConfirming(false);
        onChange("block");
      }}
      onCancel={() => setConfirming(false)}
    >
      <span>
        <Segmented
          aria-label={`${label} mode`}
          name={`mode-${monitor}`}
          disabled={readOnly || busy}
          value={current}
          options={MODES.map((m) => {
            const off = observeOnly && m !== "observe";
            return {
              value: m,
              disabled: off,
              label: off ? (
                <Tooltip title={OBSERVE_ONLY_HINT}>
                  <span>{MODE_LABEL[m]}</span>
                </Tooltip>
              ) : (
                MODE_LABEL[m]
              ),
            };
          })}
          onChange={(next) => {
            if (next === current) return;
            if (next === "block") setConfirming(true);
            else onChange(next);
          }}
        />
      </span>
    </Popconfirm>
  );
}
