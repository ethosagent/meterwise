import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { permissionsKey, refreshAccess } from "./access";
import { ApiError } from "./errors";

/**
 * The app's query client. A 403 `forbidden` from any call can mean the permission snapshot the UI
 * held was stale, so refetch the snapshot, the workspace list and Me. That refresh is worth doing
 * only while the snapshot could still change: if it is unchanged since the last refresh, a
 * further 403 is a real refusal and refreshing again would cycle (a persistent 403 on any of the
 * refreshed queries). So a refresh is skipped when the snapshot equals the one the last refresh
 * started from. The snapshot's own query is exempt.
 */
export function createQueryClient(retry: number | false): QueryClient {
  let lastRefreshedFrom: string | undefined;

  const onForbidden = (error: unknown) => {
    if (!(error instanceof ApiError && error.status === 403 && error.code === "forbidden")) return;
    const snapshot = JSON.stringify(client.getQueryData(permissionsKey) ?? null);
    if (snapshot === lastRefreshedFrom) return;
    lastRefreshedFrom = snapshot;
    void refreshAccess(client);
  };
  const client: QueryClient = new QueryClient({
    defaultOptions: { queries: { retry, refetchOnWindowFocus: false } },
    queryCache: new QueryCache({
      onError: (error, query) => {
        if (query.queryKey[0] !== permissionsKey[0]) onForbidden(error);
      },
    }),
    mutationCache: new MutationCache({ onError: onForbidden }),
  });
  resets.set(client, () => {
    lastRefreshedFrom = undefined;
  });
  return client;
}

const resets = new WeakMap<QueryClient, () => void>();

/**
 * Forget which snapshot the last 403 refresh started from. Call it when the identity changes,
 * next to clearing the cache: the dedupe is about one person's snapshot, and another person's
 * byte-identical snapshot must still get its first refresh.
 */
export function resetForbiddenRefresh(client: QueryClient) {
  resets.get(client)?.();
}
