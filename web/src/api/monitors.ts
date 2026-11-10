import { useQuery } from "@tanstack/react-query";
import type { MonitorConfig, MonitorConfigPatch, MonitorStats } from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";
import { auditLogKey } from "./setup";

export type StatsRange = "7d" | "30d";

export const monitorConfigKey = (workspaceId: string) =>
  workspaceScoped("monitors", workspaceId, "config");
export const monitorStatsKey = (workspaceId: string, range: StatsRange) =>
  workspaceScoped("monitors", workspaceId, "stats", range);

/** The workspace's monitor settings with defaults filled in (monitor_config:read). */
export function useMonitorConfig(workspaceId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: monitorConfigKey(workspaceId ?? ""),
    enabled: enabled && workspaceId !== null,
    queryFn: () => apiFetch<MonitorConfig>(`/api/workspaces/${enc(workspaceId ?? "")}/monitors`),
  });
}

/** Per-monitor fires and savings for the range (savings:read). */
export function useMonitorStats(workspaceId: string | null, range: StatsRange, enabled: boolean) {
  return useQuery({
    queryKey: monitorStatsKey(workspaceId ?? "", range),
    enabled: enabled && workspaceId !== null,
    queryFn: () =>
      apiFetch<MonitorStats>(
        `/api/workspaces/${enc(workspaceId ?? "")}/monitors/stats?range=${range}`,
      ),
  });
}

/** PATCH any subset of the settings. Refreshes the config, every stats range and the audit log it writes to. */
export function usePatchMonitors(workspaceId: string) {
  return useInvalidatingMutation(
    (patch: MonitorConfigPatch) =>
      apiFetch<MonitorConfig>(`/api/workspaces/${enc(workspaceId)}/monitors`, {
        method: "PATCH",
        body: json(patch),
      }),
    [workspaceScoped("monitors", workspaceId), auditLogKey],
  );
}
