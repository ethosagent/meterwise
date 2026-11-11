import { useQuery } from "@tanstack/react-query";
import type { MemoryConfigPatch, MemorySettings } from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";
import { auditLogKey } from "./setup";

export const memoryConfigKey = (workspaceId: string) =>
  workspaceScoped("memory", workspaceId, "config");

/** The workspace's memory settings and whether memory is enabled (monitor_config:read). */
export function useMemoryConfig(workspaceId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: memoryConfigKey(workspaceId ?? ""),
    enabled: enabled && workspaceId !== null,
    queryFn: () => apiFetch<MemorySettings>(`/api/workspaces/${enc(workspaceId ?? "")}/memory`),
  });
}

/** PATCH any subset of the memory settings. Refreshes the settings, the savings figures (the cause row shows the mode) and the audit log. */
export function usePatchMemory(workspaceId: string) {
  return useInvalidatingMutation(
    (patch: MemoryConfigPatch) =>
      apiFetch<MemorySettings>(`/api/workspaces/${enc(workspaceId)}/memory`, {
        method: "PATCH",
        body: json(patch),
      }),
    [workspaceScoped("memory", workspaceId), workspaceScoped("savings", workspaceId), auditLogKey],
  );
}
