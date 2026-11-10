import { Button, Drawer, Flex, InputNumber, Switch, Typography, theme } from "antd";
import { useState } from "react";
import type { MonitorConfig } from "../../contract/generated";
import { type MonitorKey, THRESHOLDS, monitorAbout, monitorLabel } from "./monitorMeta";

type Values = Record<string, number | boolean>;

function current(config: MonitorConfig, monitor: MonitorKey): Values {
  const out: Values = {};
  const src = config[monitor] as unknown as Record<string, unknown>;
  for (const t of THRESHOLDS[monitor]) {
    const v = src[t.field];
    if (typeof v === "number" || typeof v === "boolean") out[t.field] = v;
  }
  return out;
}

/** The thresholds of one monitor. Save sends only the fields that changed, as a partial PATCH body. */
export function ThresholdsDrawer({
  monitor,
  config,
  readOnly,
  saving,
  onClose,
  onSave,
}: {
  monitor: MonitorKey;
  config: MonitorConfig;
  readOnly: boolean;
  saving: boolean;
  onClose: () => void;
  onSave: (changed: Values) => void;
}) {
  const { token } = theme.useToken();
  const base = current(config, monitor);
  const [values, setValues] = useState<Values>(base);
  const changed = Object.fromEntries(Object.entries(values).filter(([k, v]) => v !== base[k]));
  const dirty = Object.keys(changed).length > 0;
  return (
    <Drawer
      open
      onClose={onClose}
      title={`${monitorLabel(monitor)} thresholds`}
      footer={
        <Flex justify="end" gap={token.marginXS}>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="primary"
            disabled={readOnly || !dirty}
            loading={saving}
            onClick={() => onSave(changed)}
          >
            Save
          </Button>
        </Flex>
      }
    >
      <Typography.Paragraph type="secondary">{monitorAbout(monitor)}</Typography.Paragraph>
      <Flex vertical gap={token.marginMD}>
        {THRESHOLDS[monitor].map((t) => (
          <Flex key={t.field} justify="space-between" align="center" gap={token.marginSM}>
            <Typography.Text id={`th-${t.field}`}>{t.label}</Typography.Text>
            {t.kind === "boolean" ? (
              <Switch
                aria-labelledby={`th-${t.field}`}
                disabled={readOnly}
                checked={values[t.field] === true}
                onChange={(v) => setValues({ ...values, [t.field]: v })}
              />
            ) : (
              <InputNumber
                aria-labelledby={`th-${t.field}`}
                min={0}
                precision={0}
                disabled={readOnly}
                value={typeof values[t.field] === "number" ? (values[t.field] as number) : null}
                onChange={(v) => {
                  if (typeof v === "number") setValues({ ...values, [t.field]: v });
                }}
              />
            )}
          </Flex>
        ))}
      </Flex>
    </Drawer>
  );
}
