import { Alert, Flex, Skeleton, theme } from "antd";
import { useRangeParams } from "../../api/range";
import { useSessions } from "../../api/sessions";
import { useToastOnError } from "../../api/useToastOnError";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { WorkspaceGuard } from "../../shell/WorkspaceGuard";
import { Filters, useSessionFilters } from "./Filters";
import { SessionsTable } from "./SessionsTable";

function List({ workspaceId }: { workspaceId: string }) {
  const { token } = theme.useToken();
  const { sel } = useRangeParams();
  const filters = useSessionFilters();
  const q = useSessions(workspaceId, sel, filters, true);
  useToastOnError(q.error, "Could not load sessions.");
  return (
    <Flex vertical gap={token.marginMD}>
      <Filters filters={filters} />
      {q.isError ? (
        <Alert type="error" showIcon title="Could not load sessions." />
      ) : q.data ? (
        <SessionsTable
          rows={q.data.pages.flatMap((p) => p.items)}
          hasMore={q.hasNextPage}
          loadingMore={q.isFetchingNextPage}
          onMore={() => void q.fetchNextPage()}
          filtered={filters.client !== "" || filters.outcome !== ""}
        />
      ) : (
        <Skeleton active />
      )}
    </Flex>
  );
}

/** Sessions (session:read): a filterable table. Range, client and outcome live in the URL search params. */
export function SessionsPage() {
  return (
    <section aria-labelledby="page-title">
      <PageHead page={pageByKey("sessions")} />
      <WorkspaceGuard permission="session:read" what="sessions">
        {(ws) => <List workspaceId={ws.id} />}
      </WorkspaceGuard>
    </section>
  );
}
