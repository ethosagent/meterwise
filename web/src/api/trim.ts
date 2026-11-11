import { useQuery } from "@tanstack/react-query";
import type { TrimConfig, TrimConfigPatch, TrimPreview, TrimStats } from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";
import { type RangeSel, rangeKey, rangeQuery } from "./range";
import { auditLogKey } from "./setup";

export const trimConfigKey = (workspaceId: string) =>
  workspaceScoped("trim", workspaceId, "config");

/** The workspace's trim settings (monitor_config:read). */
export function useTrimConfig(workspaceId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: trimConfigKey(workspaceId ?? ""),
    enabled: enabled && workspaceId !== null,
    queryFn: () => apiFetch<TrimConfig>(`/api/workspaces/${enc(workspaceId ?? "")}/trim`),
  });
}

/** PATCH any subset of the trim settings. Refreshes the settings, the savings figures (the cause row shows the mode) and the audit log. */
export function usePatchTrim(workspaceId: string) {
  return useInvalidatingMutation(
    (patch: TrimConfigPatch) =>
      apiFetch<TrimConfig>(`/api/workspaces/${enc(workspaceId)}/trim`, {
        method: "PATCH",
        body: json(patch),
      }),
    [workspaceScoped("trim", workspaceId), workspaceScoped("savings", workspaceId), auditLogKey],
  );
}

/** Trim figures change slowly: a remount within this window reuses the cache. */
export const TRIM_STALE_MS = 30_000;

export const trimStatsKey = (workspaceId: string, sel: RangeSel) =>
  workspaceScoped("trim", workspaceId, "stats", rangeKey(sel));

/** What trimming did over the range: certain, likely and would-save apart, plus the by-stage and by-tool tables (savings:read). */
export function useTrimStats(workspaceId: string | null, sel: RangeSel, enabled: boolean) {
  return useQuery({
    queryKey: trimStatsKey(workspaceId ?? "", sel),
    enabled: enabled && workspaceId !== null,
    staleTime: TRIM_STALE_MS,
    queryFn: () =>
      apiFetch<TrimStats>(
        `/api/workspaces/${enc(workspaceId ?? "")}/trim/stats?${rangeQuery(sel)}`,
      ),
  });
}

/** What trimming did to one call: sizes and our fixed markers only, never request text (session:read). Loads only while `enabled`. */
export function useTrimPreview(workspaceId: string | null, callId: string, enabled: boolean) {
  return useQuery({
    queryKey: workspaceScoped("trim", workspaceId ?? "", "preview", callId),
    enabled: enabled && workspaceId !== null,
    staleTime: Number.POSITIVE_INFINITY,
    queryFn: () =>
      apiFetch<TrimPreview>(`/api/workspaces/${enc(workspaceId ?? "")}/calls/${enc(callId)}/trim`),
  });
}
