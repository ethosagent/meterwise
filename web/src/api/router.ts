import { useQuery } from "@tanstack/react-query";
import type { RouterSettings, RouterConfigPatch } from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";
import { auditLogKey } from "./setup";

export const routerConfigKey = (workspaceId: string) =>
  workspaceScoped("router", workspaceId, "config");

/** The workspace's router settings with the default, suggested and effective model pairs (monitor_config:read). */
export function useRouterConfig(workspaceId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: routerConfigKey(workspaceId ?? ""),
    enabled: enabled && workspaceId !== null,
    queryFn: () => apiFetch<RouterSettings>(`/api/workspaces/${enc(workspaceId ?? "")}/router`),
  });
}

/** PATCH any subset of the router settings. Refreshes the settings, the savings figures (the cause row shows the mode) and the audit log. */
export function usePatchRouter(workspaceId: string) {
  return useInvalidatingMutation(
    (patch: RouterConfigPatch) =>
      apiFetch<RouterSettings>(`/api/workspaces/${enc(workspaceId)}/router`, {
        method: "PATCH",
        body: json(patch),
      }),
    [workspaceScoped("router", workspaceId), workspaceScoped("savings", workspaceId), auditLogKey],
  );
}
