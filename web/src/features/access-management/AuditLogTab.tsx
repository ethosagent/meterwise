import { Alert, Button, Flex, Select, Table, Typography, theme } from "antd";
import { useState } from "react";
import { AUDIT_ACTIONS, type AuditAction, type AuditEntry } from "../../api/contract04";
import { useWorkspaces } from "../../api/identity";
import { useAuditLog } from "../../api/setup";

const ACTION_OPTIONS = AUDIT_ACTIONS.map((a) => ({ value: a, label: a }));

function actorText(e: AuditEntry): string {
  return e.actor ? e.actor.name || e.actor.email : "System";
}

/** Settings > Access management > Audit log: who did what, newest first, filterable, cursor "Load more". Needs audit_log:read. */
export function AuditLogTab() {
  const { token } = theme.useToken();
  const [action, setAction] = useState<AuditAction | undefined>();
  const [workspaceId, setWorkspaceId] = useState<string | undefined>();
  const workspaces = useWorkspaces().data ?? [];
  const log = useAuditLog({ action, workspaceId }, true);
  const rows = log.data?.pages.flatMap((p) => p.items) ?? [];
  const wsName = (id: string | null) =>
    id ? (workspaces.find((w) => w.id === id)?.name ?? id) : null;
  return (
    <>
      <Flex wrap gap={token.marginSM} style={{ marginBottom: token.marginMD }}>
        <Select<AuditAction | null>
          allowClear
          aria-label="Filter by action"
          placeholder="All actions"
          style={{ minWidth: token.controlHeightLG * 6 }}
          options={ACTION_OPTIONS}
          value={action ?? null}
          onChange={(v) => setAction(v ?? undefined)}
        />
        <Select<string | null>
          allowClear
          aria-label="Filter by workspace"
          placeholder="All workspaces"
          style={{ minWidth: token.controlHeightLG * 5 }}
          options={workspaces.map((w) => ({ value: w.id, label: w.name }))}
          value={workspaceId ?? null}
          onChange={(v) => setWorkspaceId(v ?? undefined)}
        />
      </Flex>
      {log.isError ? <Alert type="error" showIcon title="Could not load the audit log." /> : null}
      <Table<AuditEntry>
        rowKey="id"
        loading={log.isLoading}
        dataSource={rows}
        pagination={false}
        scroll={{ x: "max-content" }}
        locale={{ emptyText: "No audit entries." }}
        columns={[
          { title: "When", dataIndex: "at", render: (v: string) => new Date(v).toLocaleString() },
          { title: "Actor", key: "actor", render: (_: unknown, e) => actorText(e) },
          { title: "Action", dataIndex: "action", render: (a: string) => <code>{a}</code> },
          {
            title: "Resource",
            key: "resource",
            render: (_: unknown, e) => (
              <>
                <Typography.Text>{e.resourceType}</Typography.Text>
                {e.resourceId ? (
                  <Typography.Text type="secondary">
                    {" "}
                    {wsName(e.resourceId) ?? e.resourceId}
                  </Typography.Text>
                ) : null}
              </>
            ),
          },
          { title: "IP", dataIndex: "ip", render: (v: string | null) => v ?? "—" },
        ]}
      />
      {log.hasNextPage ? (
        <Flex justify="center" style={{ marginTop: token.marginMD }}>
          <Button loading={log.isFetchingNextPage} onClick={() => log.fetchNextPage()}>
            Load more
          </Button>
        </Flex>
      ) : null}
    </>
  );
}
