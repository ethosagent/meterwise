import { Button, Empty, Table, Tag } from "antd";
import { Link } from "react-router-dom";
import type { Session } from "../../contract/generated";
import { formatTokens, formatUsd } from "../../lib/money";
import { LoadMore } from "../../shell/LoadMore";

/** The sessions list: one row per session, newest first, each linking to its drill-down. */
export function SessionsTable({
  rows,
  hasMore,
  loadingMore,
  onMore,
  filtered,
}: {
  rows: Session[];
  hasMore: boolean;
  loadingMore: boolean;
  onMore: () => void;
  filtered: boolean;
}) {
  return (
    <>
      <Table<Session>
        rowKey="id"
        size="small"
        pagination={false}
        dataSource={rows}
        locale={{
          emptyText: (
            <Empty
              description={
                filtered ? "No sessions match these filters" : "No sessions in this range yet"
              }
            >
              {filtered ? null : (
                <Link to="/setup">
                  <Button>Go to Setup</Button>
                </Link>
              )}
            </Empty>
          ),
        }}
        columns={[
          {
            title: "Started (UTC)",
            render: (_, r) => (
              <Link to={`/sessions/${encodeURIComponent(r.id)}`}>
                {r.startedAt.replace("T", " ").replace("Z", "")}
              </Link>
            ),
          },
          { title: "Client", render: (_, r) => r.client ?? "unknown" },
          { title: "Calls", dataIndex: "calls" },
          {
            title: "Tokens in / out",
            render: (_, r) => `${formatTokens(r.inputTokens)} / ${formatTokens(r.outputTokens)}`,
          },
          {
            title: "Spent",
            render: (_, r) => (r.costUsd === null ? "unpriced" : formatUsd(r.costUsd)),
          },
          { title: "Saved", render: (_, r) => formatUsd(r.savedUsd) },
          { title: "Likely saved", render: (_, r) => formatUsd(r.likelySavedUsd) },
          { title: "Outcome", render: (_, r) => <Tag>{r.outcome}</Tag> },
        ]}
      />
      <LoadMore hasMore={hasMore} loading={loadingMore} onMore={onMore} />
    </>
  );
}
