import { Alert, Flex, Skeleton, Typography, theme } from "antd";
import { useState } from "react";
import {
  type StatsRange,
  useMonitorConfig,
  useMonitorStats,
  usePatchMonitors,
} from "../../api/monitors";
import { useRangeParams } from "../../api/range";
import { useErrorToast } from "../../api/useErrorToast";
import { useToastOnError } from "../../api/useToastOnError";
import { usePermission } from "../../auth/usePermission";
import type { MonitorConfigPatch } from "../../contract/generated";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { RangeControl } from "../../shell/RangeControl";
import { WorkspaceGuard } from "../../shell/WorkspaceGuard";
import { MonitorsTable } from "./MonitorsTable";
import { type MonitorKey, type MonitorMode, monitorLabel } from "./monitorMeta";
import { ThresholdsDrawer } from "./ThresholdsDrawer";

function Settings({ workspaceId }: { workspaceId: string }) {
  const { token } = theme.useToken();
  const { sel, setRange } = useRangeParams();
  const range: StatsRange = sel.range === "30d" ? "30d" : "7d";
  const canWrite = usePermission("monitor_config:write", workspaceId);
  const canStats = usePermission("savings:read", workspaceId);
  const config = useMonitorConfig(workspaceId, true);
  const stats = useMonitorStats(workspaceId, range, canStats);
  const patch = usePatchMonitors(workspaceId);
  const toast = useErrorToast();
  const [editing, setEditing] = useState<MonitorKey | null>(null);
  useToastOnError(config.error, "Could not load the monitor settings.");
  useToastOnError(stats.error, "Could not load the monitor figures.");

  const send = (
    monitor: MonitorKey,
    fields: Record<string, number | boolean | MonitorMode>,
    done?: () => void,
  ) =>
    patch.mutate({ [monitor]: fields } as MonitorConfigPatch, {
      onSuccess: () => done?.(),
      onError: (e) => toast(e, `Could not update ${monitorLabel(monitor)}.`),
    });

  if (config.isError)
    return <Alert type="error" showIcon title="Could not load the monitor settings." />;
  if (!config.data) return <Skeleton active />;
  return (
    <Flex vertical gap={token.marginMD}>
      <RangeControl custom={false} value={{ ...sel, range }} onChange={setRange} />
      {canWrite ? null : (
        <Typography.Text type="secondary">
          You can see these settings but not change them. Changing a monitor needs
          monitor_config:write.
        </Typography.Text>
      )}
      <MonitorsTable
        config={config.data}
        stats={canStats ? stats.data?.monitors : undefined}
        readOnly={!canWrite}
        busy={patch.isPending}
        onMode={(monitor, mode) => send(monitor, { mode })}
        onThresholds={setEditing}
      />
      {editing ? (
        <ThresholdsDrawer
          monitor={editing}
          config={config.data}
          readOnly={!canWrite}
          saving={patch.isPending}
          onClose={() => setEditing(null)}
          onSave={(changed) => send(editing, changed, () => setEditing(null))}
        />
      ) : null}
    </Flex>
  );
}

/** Monitors (monitor_config:read): what is on and off. Writing a mode needs monitor_config:write; the figures need savings:read. */
export function MonitorsPage() {
  return (
    <section aria-labelledby="page-title">
      <PageHead page={pageByKey("monitors")} />
      <WorkspaceGuard permission="monitor_config:read" what="monitor settings">
        {(ws) => <Settings workspaceId={ws.id} />}
      </WorkspaceGuard>
    </section>
  );
}
