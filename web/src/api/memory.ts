import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type {
  MemoryConfigPatch,
  MemoryHitView,
  MemorySettings,
  MemoryStats,
  TraceDetail,
  TraceList,
} from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";
import { type RangeSel, rangeKey, rangeQuery } from "./range";
import { sessionDetailKey } from "./sessions";
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

export const TRACES_PAGE = 25;

export const memoryStatsKey = (workspaceId: string, sel: RangeSel) =>
  workspaceScoped("memory", workspaceId, "stats", rangeKey(sel));

/** What memory did over the range: traces, hinted sessions, feedback and the likely saving, never a certain one (savings:read). */
export function useMemoryStats(workspaceId: string | null, sel: RangeSel, enabled: boolean) {
  return useQuery({
    queryKey: memoryStatsKey(workspaceId ?? "", sel),
    enabled: enabled && workspaceId !== null,
    queryFn: () =>
      apiFetch<MemoryStats>(
        `/api/workspaces/${enc(workspaceId ?? "")}/memory/stats?${rangeQuery(sel)}`,
      ),
  });
}

/** The workspace's remembered traces, newest first, paged by the server's cursor (session:read). */
export function useTraces(workspaceId: string | null, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: workspaceScoped("memory", workspaceId ?? "", "traces"),
    enabled: enabled && workspaceId !== null,
    initialPageParam: null as string | null,
    getNextPageParam: (last: TraceList) => last.nextCursor,
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams({ limit: String(TRACES_PAGE) });
      if (pageParam) q.set("cursor", pageParam);
      return apiFetch<TraceList>(`/api/workspaces/${enc(workspaceId ?? "")}/traces?${q}`);
    },
  });
}

/** One trace with its solution and the sessions it helped; loads only while `enabled` (session:read). */
export function useTrace(workspaceId: string | null, traceId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: workspaceScoped("memory", workspaceId ?? "", "trace", traceId ?? ""),
    enabled: enabled && workspaceId !== null && traceId !== null,
    queryFn: () =>
      apiFetch<TraceDetail>(
        `/api/workspaces/${enc(workspaceId ?? "")}/traces/${enc(traceId ?? "")}`,
      ),
  });
}

export type HintFeedback = "helpful" | "irrelevant" | null;

/** 👍/👎 on one hint; null clears it. Refreshes the session (the stored value is what shows) and memory's figures. */
export function useMemoryHitFeedback(workspaceId: string, sessionId: string) {
  return useInvalidatingMutation(
    (v: { hitId: string; feedback: HintFeedback }) =>
      apiFetch<MemoryHitView>(
        `/api/workspaces/${enc(workspaceId)}/memory-hits/${enc(v.hitId)}/feedback`,
        { method: "POST", body: json({ feedback: v.feedback }) },
      ),
    [sessionDetailKey(workspaceId, sessionId), workspaceScoped("memory", workspaceId)],
  );
}

/** "Forget memory": deletes every trace of the workspace (workspace:manage, audited). */
export function useForgetMemory(workspaceId: string) {
  return useInvalidatingMutation(
    () => apiFetch<undefined>(`/api/workspaces/${enc(workspaceId)}/traces`, { method: "DELETE" }),
    [workspaceScoped("memory", workspaceId), workspaceScoped("savings", workspaceId), auditLogKey],
  );
}
