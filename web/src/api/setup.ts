import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";
import type { AuditAction, AuditPage, ResetUrlResult, Setup } from "./contract04";
import { workspaceScoped, workspacesKey } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";

/** Every Setup query (one per workspace) sits under this key, so one invalidation refreshes them all. */
export const setupRootKey = workspaceScoped("setup");
export const setupKey = (workspaceId: string) => workspaceScoped("setup", workspaceId);
export const auditLogKey = ["audit-log"] as const;

/** How often an open Setup page asks again, so "waiting for first call" flips to "connected" by itself. */
export const SETUP_POLL_MS = 15_000;

export function useSetup(workspaceId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: setupKey(workspaceId ?? ""),
    enabled: enabled && workspaceId !== null,
    refetchInterval: SETUP_POLL_MS,
    queryFn: () => apiFetch<Setup>(`/api/workspaces/${enc(workspaceId ?? "")}/setup`),
  });
}

/** After a URL change the Setup snippets, the workspace list (it carries the URL) and the audit log are stale. */
const URL_KEYS = [setupRootKey, workspacesKey, auditLogKey] as const;

/** Issue a new URL; the old one keeps working for `graceMinutes` (server default 10). */
export function useResetUrl() {
  return useInvalidatingMutation(
    (v: { workspaceId: string; graceMinutes: number }) =>
      apiFetch<ResetUrlResult>(`/api/workspaces/${enc(v.workspaceId)}/url/reset`, {
        method: "POST",
        body: json({ graceMinutes: v.graceMinutes }),
      }),
    URL_KEYS,
  );
}

/** End the grace window of every previous URL now. */
export function useRetirePrevious() {
  return useInvalidatingMutation(
    (workspaceId: string) =>
      apiFetch<void>(`/api/workspaces/${enc(workspaceId)}/url/retire-previous`, { method: "POST" }),
    [auditLogKey],
  );
}

/** Replace the allowed client CIDRs; an empty list clears the restriction. */
export function useSetAllowedCidrs() {
  return useInvalidatingMutation(
    (v: { workspaceId: string; cidrs: string[] }) =>
      apiFetch<{ cidrs: string[] }>(`/api/workspaces/${enc(v.workspaceId)}/url/allowed-cidrs`, {
        method: "PUT",
        body: json({ cidrs: v.cidrs }),
      }),
    [workspacesKey, setupRootKey, auditLogKey],
  );
}

export interface AuditFilters {
  action?: AuditAction | undefined;
  workspaceId?: string | undefined;
}

/** The audit log, newest first, one page per request; `fetchNextPage` follows the opaque cursor. */
export function useAuditLog(filters: AuditFilters, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: [...auditLogKey, filters],
    enabled,
    initialPageParam: null as string | null,
    getNextPageParam: (last: AuditPage) => last.nextCursor,
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams();
      if (pageParam) q.set("cursor", pageParam);
      if (filters.action) q.set("action", filters.action);
      if (filters.workspaceId) q.set("workspaceId", filters.workspaceId);
      const qs = q.toString();
      return apiFetch<AuditPage>(`/api/audit-log${qs ? `?${qs}` : ""}`);
    },
  });
}
