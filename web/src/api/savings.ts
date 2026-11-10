import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { SaveList, SavingsCauses, SavingsOverview } from "../contract/generated";
import { apiFetch } from "./client";
import { workspaceScoped } from "./identity";
import { enc } from "./mutation";
import { type RangeSel, rangeKey, rangeQuery } from "./range";

/** Every savings query sits under `["ws", "savings", workspaceId, view, rangeKey]` so a switch refreshes them all. */
export const savingsKey = (workspaceId: string, view: string, sel: RangeSel) =>
  workspaceScoped("savings", workspaceId, view, rangeKey(sel));

/** How often an open Overview asks again while no call has been seen, so "waiting" flips to "collecting" by itself. */
export const SAVINGS_POLL_MS = 15_000;
/** Savings figures change slowly: a tab switch or remount within this window reuses the cache. */
export const SAVINGS_STALE_MS = 30_000;
/** Rows per page of the biggest-saves table. */
export const SAVES_PAGE = 20;

const base = (workspaceId: string, view: string) =>
  `/api/workspaces/${enc(workspaceId)}/savings/${view}`;

/** Hero, spend series, by-cause bars and upsell for the range. Pass `enabled=false` without savings:read. */
export function useSavingsOverview(workspaceId: string | null, sel: RangeSel, enabled: boolean) {
  return useQuery({
    queryKey: savingsKey(workspaceId ?? "", "overview", sel),
    enabled: enabled && workspaceId !== null,
    staleTime: SAVINGS_STALE_MS,
    refetchInterval: (query) => (query.state.data?.state === "no_calls" ? SAVINGS_POLL_MS : false),
    queryFn: () =>
      apiFetch<SavingsOverview>(`${base(workspaceId ?? "", "overview")}?${rangeQuery(sel)}`),
  });
}

/** Per-cause cards, the client table and the prompt suggestions. */
export function useSavingsCauses(workspaceId: string | null, sel: RangeSel, enabled: boolean) {
  return useQuery({
    queryKey: savingsKey(workspaceId ?? "", "causes", sel),
    enabled: enabled && workspaceId !== null,
    staleTime: SAVINGS_STALE_MS,
    queryFn: () =>
      apiFetch<SavingsCauses>(`${base(workspaceId ?? "", "causes")}?${rangeQuery(sel)}`),
  });
}

/** The biggest saves, one page per request; `fetchNextPage` follows the opaque cursor. */
export function useSaves(workspaceId: string | null, sel: RangeSel, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: savingsKey(workspaceId ?? "", "saves", sel),
    enabled: enabled && workspaceId !== null,
    staleTime: SAVINGS_STALE_MS,
    initialPageParam: null as string | null,
    getNextPageParam: (last: SaveList) => last.nextCursor,
    queryFn: ({ pageParam }) => {
      const q = rangeQuery(sel);
      q.set("limit", String(SAVES_PAGE));
      if (pageParam) q.set("cursor", pageParam);
      return apiFetch<SaveList>(`${base(workspaceId ?? "", "saves")}?${q}`);
    },
  });
}
