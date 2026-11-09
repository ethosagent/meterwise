import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { permissionsKey, refreshAccess } from "./access";
import { ApiError } from "./errors";

/**
 * The app's query client. A 403 `forbidden` from any call means the permission snapshot the UI
 * held was stale (a grant was revoked elsewhere), so refetch the snapshot, the workspace list and
 * Me. The snapshot's own query is exempt, or a 403 on it would loop.
 */
export function createQueryClient(retry: number | false): QueryClient {
  const client: QueryClient = new QueryClient({
    defaultOptions: { queries: { retry, refetchOnWindowFocus: false } },
    queryCache: new QueryCache({
      onError: (error, query) => {
        if (query.queryKey[0] === permissionsKey[0]) return;
        onForbidden(client, error);
      },
    }),
    mutationCache: new MutationCache({ onError: (error) => onForbidden(client, error) }),
  });
  return client;
}

function onForbidden(client: QueryClient, error: unknown) {
  if (error instanceof ApiError && error.status === 403 && error.code === "forbidden") {
    void refreshAccess(client);
  }
}
