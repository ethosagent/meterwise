import { Popconfirm, Segmented } from "antd";
import { useState } from "react";

export interface ModeOption {
  value: string;
  label: string;
}

export interface ModeConfirm {
  title: string;
  description: string;
  okText: string;
}

/**
 * A saver's mode as a segmented control. A mode named in `confirm` waits behind a Popconfirm before it is
 * sent, because it changes what reaches the provider or what the agent is told. `readOnly` (no
 * monitor_config:write) disables the whole control.
 */
export function ModeSegment({
  label,
  value,
  options,
  confirm,
  readOnly,
  busy,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly ModeOption[];
  confirm?: Record<string, ModeConfirm>;
  readOnly: boolean;
  busy: boolean;
  onChange: (next: string) => void;
}) {
  const [asking, setAsking] = useState<string | null>(null);
  const ask = asking ? confirm?.[asking] : undefined;
  return (
    <Popconfirm
      open={ask !== undefined}
      title={ask?.title}
      description={ask?.description}
      okText={ask?.okText}
      cancelText="Cancel"
      onConfirm={() => {
        const next = asking;
        setAsking(null);
        if (next) onChange(next);
      }}
      onCancel={() => setAsking(null)}
    >
      <span>
        <Segmented
          aria-label={label}
          name={`mode-${label}`}
          disabled={readOnly || busy}
          value={value}
          options={options.map((o) => ({ value: o.value, label: o.label }))}
          onChange={(next) => {
            if (next === value) return;
            if (confirm?.[next]) setAsking(next);
            else onChange(next);
          }}
        />
      </span>
    </Popconfirm>
  );
}
