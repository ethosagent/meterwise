import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { MonitorEvent, SessionDetail, SessionList } from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc, json, useInvalidatingMutation } from "./mutation";
import { type RangeSel, rangeBounds, rangeKey } from "./range";

/** The route of one session's detail page; `query` adds `?event=` or `?call=` to deep-link a step. */
export const sessionPath = (sessionId: string, query?: { event?: string; call?: string }) => {
  const q = new URLSearchParams();
  if (query?.event) q.set("event", query.event);
  if (query?.call) q.set("call", query.call);
  const qs = q.toString();
  return `/sessions/${enc(sessionId)}${qs ? `?${qs}` : ""}`;
};

export const SESSION_OUTCOMES = ["completed", "abandoned", "open"] as const;
export type SessionOutcome = (typeof SESSION_OUTCOMES)[number];

export const SESSION_TAGS = ["trimmed", "routed", "remembered"] as const;

export interface SessionFilters {
  client: string;
  outcome: string;
  tag: string;
}

export const sessionsKey = (workspaceId: string, sel: RangeSel, f: SessionFilters) =>
  workspaceScoped("sessions", workspaceId, rangeKey(sel), f.client, f.outcome, f.tag);
export const sessionDetailKey = (workspaceId: string, sessionId: string) =>
  workspaceScoped("session", workspaceId, sessionId);

export const SESSIONS_PAGE = 50;

/** The workspace's sessions in the range, newest first, filtered server side. */
export function useSessions(
  workspaceId: string | null,
  sel: RangeSel,
  filters: SessionFilters,
  enabled: boolean,
) {
  return useInfiniteQuery({
    queryKey: sessionsKey(workspaceId ?? "", sel, filters),
    enabled: enabled && workspaceId !== null,
    initialPageParam: null as string | null,
    getNextPageParam: (last: SessionList) => last.nextCursor,
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams({ ...rangeBounds(sel), limit: String(SESSIONS_PAGE) });
      if (filters.client) q.set("client", filters.client);
      if (filters.outcome) q.set("outcome", filters.outcome);
      if (filters.tag) q.set("tag", filters.tag);
      if (pageParam) q.set("cursor", pageParam);
      return apiFetch<SessionList>(`/api/workspaces/${enc(workspaceId ?? "")}/sessions?${q}`);
    },
  });
}

/** One session with its calls, monitor events and insight; `workspaceId` authorizes the read. */
export function useSessionDetail(workspaceId: string | null, sessionId: string, enabled: boolean) {
  return useQuery({
    queryKey: sessionDetailKey(workspaceId ?? "", sessionId),
    enabled: enabled && workspaceId !== null,
    queryFn: () =>
      apiFetch<SessionDetail>(
        `/api/sessions/${enc(sessionId)}?${new URLSearchParams({ workspaceId: workspaceId ?? "" })}`,
      ),
  });
}

export type Feedback = "correct" | "wrong" | null;

/** 👍/👎 on a monitor event; null clears it. Refreshes the session so the stored value is what shows. */
export function useFeedback(workspaceId: string, sessionId: string) {
  return useInvalidatingMutation(
    (v: { eventId: string; feedback: Feedback }) =>
      apiFetch<MonitorEvent>(
        `/api/workspaces/${enc(workspaceId)}/monitor-events/${enc(v.eventId)}/feedback`,
        { method: "POST", body: json({ feedback: v.feedback }) },
      ),
    [sessionDetailKey(workspaceId, sessionId), workspaceScoped("monitors", workspaceId)],
  );
}
