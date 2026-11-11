import { useQuery } from "@tanstack/react-query";
import type { TrimConfig, TrimConfigPatch } from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";
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
