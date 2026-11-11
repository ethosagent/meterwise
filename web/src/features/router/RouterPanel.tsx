import { Alert, Button, Flex, InputNumber, Select, Skeleton, Table, Typography, theme } from "antd";
import { useState } from "react";
import { usePatchRouter, useRouterStats } from "../../api/router";
import { useErrorToast } from "../../api/useErrorToast";
import type { RoutePair, RouterSettings, RouterStats } from "../../contract/generated";
import { formatBp, formatUsd } from "../../lib/money";

const sameFrom = (a: RoutePair, b: RoutePair) => a.provider === b.provider && a.from === b.from;
const label = (p: RoutePair) => `${p.provider}: ${p.from} → ${p.to}`;
const MODE_DEFAULT_BP: Record<string, number> = { conservative: 2500, aggressive: 5000 };

/** The figures line: what routing saved (certain) or would save (observe), and a refused target, never summed. */
function Figures({ stats, mode }: { stats: RouterStats; mode: string }) {
  const observe = mode === "observe";
  return (
    <Flex vertical gap={4} aria-label="Routing figures">
      {observe ? (
        <Typography.Text>
          {`Would save ${formatUsd(stats.wouldSaveUsd)} over the last 7 days on ${stats.observedCalls} calls. Nothing was rerouted.`}
        </Typography.Text>
      ) : (
        <Typography.Text>
          {`Saved ${formatUsd(stats.savedUsd)} over the last 7 days: ${stats.routedCalls} of ${stats.calls} calls (${formatBp(stats.routedBp)}) went to a cheaper model.`}
        </Typography.Text>
      )}
      {stats.unavailable > 0 ? (
        <Typography.Text type="warning">
          {`${stats.unavailable} routed call${stats.unavailable === 1 ? " was" : "s were"} refused by the provider; that pair is paused for an hour and its calls stay on the model asked for.`}
        </Typography.Text>
      ) : null}
      {stats.worseFeedback > 0 ? (
        <Typography.Text type="secondary">{`${stats.worseFeedback} routed answers marked worse.`}</Typography.Text>
      ) : null}
    </Flex>
  );
}

/** The guardrail: routed calls against the rest. It warns when the cheap model's monitors fire over 2 points more often. */
function Guardrail({ stats }: { stats: RouterStats }) {
  const g = stats.guardrail;
  if (g.cheapCalls === 0) return null;
  const detail = `Monitors fired on ${formatBp(g.cheapFireBp)} of routed calls against ${formatBp(g.strongFireBp)} of the others; sessions completed ${formatBp(g.cheapCompletedBp)} against ${formatBp(g.strongCompletedBp)}.`;
  return g.warn ? (
    <Alert
      type="warning"
      showIcon
      title="Routed calls trip monitors more often than the others"
      description={`${detail} Raise the threshold less, switch to Conservative, or remove a pair below.`}
    />
  ) : (
    <Typography.Text type="secondary">{detail}</Typography.Text>
  );
}

/**
 * The finer routing settings under the mode switch: the figures, the guardrail, the threshold and the model map.
 * The map editor shows the pairs in effect (the workspace's own map, else the defaults); a pair is added from the
 * default or suggested ones, a row is removed, and "Reset to defaults" drops the workspace map.
 */
export function RouterPanel({
  workspaceId,
  settings,
  readOnly,
  canStats,
}: {
  workspaceId: string;
  settings: RouterSettings;
  readOnly: boolean;
  canStats: boolean;
}) {
  const { token } = theme.useToken();
  const toast = useErrorToast();
  const patch = usePatchRouter(workspaceId);
  const stats = useRouterStats(workspaceId, "7d", canStats && settings.config.mode !== "off");
  const [threshold, setThreshold] = useState<number | null>(settings.config.thresholdBp);
  const [pick, setPick] = useState<string | null>(null);
  const mode = settings.config.mode;
  const send = (body: Parameters<typeof patch.mutate>[0], what: string, done?: () => void) =>
    patch.mutate(body, {
      ...(done ? { onSuccess: done } : {}),
      onError: (e) => toast(e, `Could not update ${what}.`),
    });
  const addable = [...settings.suggestions, ...settings.defaults].filter(
    (p, i, all) =>
      all.findIndex((q) => label(q) === label(p)) === i &&
      !settings.effective.some((e) => sameFrom(e, p)),
  );
  const chosen = addable.find((p) => label(p) === pick);
  const effectiveDefault = MODE_DEFAULT_BP[mode];
  const dirty = threshold !== settings.config.thresholdBp;
  return (
    <Flex vertical gap={token.marginSM} aria-label="Model routing settings">
      {canStats && mode !== "off" ? (
        stats.data ? (
          <>
            <Figures stats={stats.data} mode={mode} />
            <Guardrail stats={stats.data} />
          </>
        ) : stats.isError ? null : (
          <Skeleton active paragraph={{ rows: 1 }} />
        )
      ) : null}
      <Flex gap={token.marginXS} align="center" wrap>
        <Typography.Text strong>Threshold</Typography.Text>
        <InputNumber
          aria-label="Routing threshold"
          min={1}
          max={10000}
          step={100}
          value={threshold}
          placeholder={effectiveDefault === undefined ? "mode default" : String(effectiveDefault)}
          disabled={readOnly || patch.isPending}
          onChange={(v) => setThreshold(v)}
        />
        <Typography.Text type="secondary">
          basis points of the simplicity score; a call scoring at or under it may be rerouted. Empty
          is the mode's default.
        </Typography.Text>
        <Button
          size="small"
          disabled={readOnly || !dirty || patch.isPending}
          onClick={() => send({ thresholdBp: threshold ?? 0 }, "the threshold")}
        >
          Save threshold
        </Button>
      </Flex>
      <Table<RoutePair>
        size="small"
        aria-label="Model map"
        pagination={false}
        rowKey={label}
        dataSource={settings.effective}
        locale={{ emptyText: "No pairs: nothing is rerouted." }}
        title={() => (
          <Typography.Text strong>
            {settings.config.map === null ? "Model map (defaults)" : "Model map (this workspace)"}
          </Typography.Text>
        )}
        columns={[
          { title: "Provider", dataIndex: "provider" },
          { title: "Asked for", dataIndex: "from" },
          { title: "Sent to", dataIndex: "to" },
          {
            title: "",
            key: "remove",
            render: (_: unknown, row: RoutePair) => (
              <Button
                size="small"
                danger
                aria-label={`Remove ${label(row)}`}
                disabled={readOnly || patch.isPending}
                onClick={() =>
                  send(
                    { map: settings.effective.filter((p) => !sameFrom(p, row)) },
                    "the model map",
                  )
                }
              >
                Remove
              </Button>
            ),
          },
        ]}
      />
      <Flex gap={token.marginXS} wrap align="center">
        <Select
          aria-label="Add a model pair"
          style={{ minWidth: token.controlHeightLG * 7 }}
          placeholder="Add a suggested or default pair"
          disabled={readOnly || addable.length === 0}
          value={pick}
          onChange={setPick}
          options={addable.map((p) => ({
            value: label(p),
            label: `${label(p)}${settings.suggestions.some((s) => label(s) === label(p)) ? " (seen in your calls)" : ""}`,
          }))}
        />
        <Button
          disabled={readOnly || !chosen || patch.isPending}
          onClick={() =>
            chosen &&
            send({ map: [...settings.effective, chosen] }, "the model map", () => setPick(null))
          }
        >
          Add pair
        </Button>
        <Button
          disabled={readOnly || settings.config.map === null || patch.isPending}
          onClick={() => send({ resetMap: true }, "the model map")}
        >
          Reset to defaults
        </Button>
      </Flex>
    </Flex>
  );
}
