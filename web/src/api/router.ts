import { useQuery } from "@tanstack/react-query";
import type {
  RouterConfigPatch,
  RouterFeedbackView,
  RouterSettings,
  RouterStats,
} from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";
import { sessionDetailKey } from "./sessions";
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

/** The router's own figures over the last 7 or 30 days (savings:read): saved, would-save, guardrail, per pair. */
export function useRouterStats(workspaceId: string | null, range: "7d" | "30d", enabled: boolean) {
  return useQuery({
    queryKey: workspaceScoped("router", workspaceId ?? "", "stats", range),
    enabled: enabled && workspaceId !== null,
    queryFn: () =>
      apiFetch<RouterStats>(
        `/api/workspaces/${enc(workspaceId ?? "")}/router/stats?range=${range}`,
      ),
  });
}

/** Marks one routed call "worse" or "fine" (null clears it). Refreshes the session and the router figures. */
export function useRouterFeedback(workspaceId: string, sessionId: string) {
  return useInvalidatingMutation(
    (v: { callId: string; verdict: "worse" | "fine" | null }) =>
      apiFetch<RouterFeedbackView>(
        `/api/workspaces/${enc(workspaceId)}/calls/${enc(v.callId)}/router-feedback`,
        { method: "POST", body: json({ verdict: v.verdict }) },
      ),
    [sessionDetailKey(workspaceId, sessionId), workspaceScoped("router", workspaceId)],
  );
}
