import { Button, Space, Table, Tag, Typography } from "antd";
import type { MonitorConfig, MonitorStatsRow } from "../../contract/generated";
import { formatUsd, isPositiveMicro } from "../../lib/money";
import { ModeControl } from "./ModeControl";
import {
  MONITOR_KEYS,
  type MonitorKey,
  type MonitorMode,
  OBSERVE_ONLY,
  monitorAbout,
  monitorLabel,
} from "./monitorMeta";

interface Row {
  key: MonitorKey;
  mode: string;
  stat: MonitorStatsRow | undefined;
}

const DASH = "-";

/** The six monitors: mode, fires, saved / likely / would-save (three columns, never summed), marked wrong, thresholds. */
export function MonitorsTable({
  config,
  stats,
  readOnly,
  busy,
  onMode,
  onThresholds,
}: {
  config: MonitorConfig;
  /** undefined while loading or without savings:read; the money columns then show a dash. */
  stats: MonitorStatsRow[] | undefined;
  readOnly: boolean;
  busy: boolean;
  onMode: (monitor: MonitorKey, mode: MonitorMode) => void;
  onThresholds: (monitor: MonitorKey) => void;
}) {
  const rows: Row[] = MONITOR_KEYS.map((key) => ({
    key,
    mode: config[key].mode,
    stat: stats?.find((s) => s.monitor === key),
  }));
  const money = (pick: (s: MonitorStatsRow) => string) => (_: unknown, r: Row) =>
    r.stat ? formatUsd(pick(r.stat)) : DASH;
  return (
    <Table<Row>
      rowKey="key"
      size="small"
      pagination={false}
      dataSource={rows}
      columns={[
        {
          title: "Monitor",
          render: (_, r) => (
            <>
              <Typography.Text strong>{monitorLabel(r.key)}</Typography.Text>
              <br />
              <Typography.Text type="secondary">{monitorAbout(r.key)}</Typography.Text>
            </>
          ),
        },
        {
          title: "Mode",
          render: (_, r) => (
            <Space>
              <ModeControl
                monitor={r.key}
                mode={r.mode}
                readOnly={readOnly}
                busy={busy}
                onChange={(m) => onMode(r.key, m)}
              />
              {r.mode === "observe" &&
              !readOnly &&
              !OBSERVE_ONLY.has(r.key) &&
              r.stat &&
              isPositiveMicro(r.stat.wouldSaveUsd) ? (
                <Button
                  size="small"
                  type="link"
                  disabled={busy}
                  onClick={() => onMode(r.key, "note")}
                >
                  Turn on
                </Button>
              ) : null}
            </Space>
          ),
        },
        { title: "Fires", render: (_, r) => r.stat?.fires ?? DASH },
        { title: "Saved", render: money((s) => s.savedUsd) },
        { title: "Likely saved", render: money((s) => s.likelySavedUsd) },
        {
          title: "Would save",
          render: (_, r) =>
            r.stat && isPositiveMicro(r.stat.wouldSaveUsd) ? (
              <Tag color="warning" variant="outlined">
                {formatUsd(r.stat.wouldSaveUsd)}
              </Tag>
            ) : r.stat ? (
              formatUsd("0")
            ) : (
              DASH
            ),
        },
        { title: "Marked wrong", render: (_, r) => r.stat?.feedbackWrong ?? DASH },
        {
          title: "",
          render: (_, r) => (
            <Button
              size="small"
              onClick={() => onThresholds(r.key)}
              aria-label={`${monitorLabel(r.key)} thresholds`}
            >
              Thresholds
            </Button>
          ),
        },
      ]}
    />
  );
}
