import { Alert, Flex, Skeleton, Typography, theme } from "antd";
import { useMemo, useState } from "react";
import { useMemoryConfig, useTraces } from "../../api/memory";
import { useToastOnError } from "../../api/useToastOnError";
import { usePermission } from "../../auth/usePermission";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { WorkspaceGuard } from "../../shell/WorkspaceGuard";
import { memoryStateText } from "./memoryText";
import { TraceDrawer } from "./TraceDrawer";
import { TracesTable } from "./TracesTable";

function Traces({ workspaceId }: { workspaceId: string }) {
  const { token } = theme.useToken();
  const canConfig = usePermission("monitor_config:read", workspaceId);
  const config = useMemoryConfig(workspaceId, canConfig);
  const q = useTraces(workspaceId, true);
  const [open, setOpen] = useState<string | null>(null);
  const rows = useMemo(() => q.data?.pages.flatMap((p) => p.items) ?? [], [q.data]);
  useToastOnError(q.error, "Could not load the remembered solutions.");
  useToastOnError(config.error, "Could not load the memory settings.");
  if (q.isError)
    return <Alert type="error" showIcon title="Could not load the remembered solutions." />;
  if (!q.data) return <Skeleton active />;
  const state = config.data ? memoryStateText(config.data, undefined) : null;
  const unavailable = config.data !== undefined && !config.data.enabled;
  return (
    <Flex vertical gap={token.marginMD}>
      {unavailable && state ? <Alert type="info" showIcon title={state} /> : null}
      <Typography.Text type="secondary">
        Solutions memory learned from completed sessions in this workspace. A trace is only ever
        offered to later sessions of the same workspace.
      </Typography.Text>
      <TracesTable
        rows={rows}
        emptyText={
          unavailable || config.isError
            ? "No remembered solutions"
            : (state ?? "Memory starts learning after your first completed session")
        }
        hasMore={q.hasNextPage}
        loadingMore={q.isFetchingNextPage}
        onMore={() => void q.fetchNextPage()}
        onOpen={setOpen}
      />
      {open ? (
        <TraceDrawer workspaceId={workspaceId} traceId={open} onClose={() => setOpen(null)} />
      ) : null}
    </Flex>
  );
}

/** Memory (session:read): the remembered solutions of this workspace, each opening a drawer with the full trace. */
export function TracesPage() {
  return (
    <section aria-labelledby="page-title">
      <PageHead page={pageByKey("memory")} />
      <WorkspaceGuard permission="session:read" what="remembered solutions">
        {(ws) => <Traces workspaceId={ws.id} />}
      </WorkspaceGuard>
    </section>
  );
}
