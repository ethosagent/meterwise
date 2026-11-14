import { Button, Empty, Table } from "antd";
import type { TraceSummary } from "../../contract/generated";
import { LoadMore } from "../../shell/LoadMore";

const SHOWN_FILES = 2;

function files(t: TraceSummary): string {
  const rest = t.files.length - SHOWN_FILES;
  return t.files.slice(0, SHOWN_FILES).join(", ") + (rest > 0 ? ` +${rest}` : "");
}

/** The traces list: one row per remembered solution, newest first; the summary opens the drawer. */
export function TracesTable({
  rows,
  emptyText,
  hasMore,
  loadingMore,
  onMore,
  onOpen,
}: {
  rows: TraceSummary[];
  emptyText: string;
  hasMore: boolean;
  loadingMore: boolean;
  onMore: () => void;
  onOpen: (id: string) => void;
}) {
  return (
    <>
      <Table<TraceSummary>
        rowKey="id"
        size="small"
        pagination={false}
        dataSource={rows}
        locale={{ emptyText: <Empty description={emptyText} /> }}
        columns={[
          {
            title: "Summary",
            render: (_, r) => (
              <Button
                type="link"
                onClick={() => onOpen(r.id)}
                style={{ padding: 0, height: "auto" }}
              >
                {r.taskSummary}
              </Button>
            ),
          },
          { title: "Files", render: (_, r) => files(r) },
          { title: "Uses", dataIndex: "uses" },
          { title: "Helped / irrelevant", render: (_, r) => `${r.upvotes} / ${r.downvotes}` },
          {
            title: "Last used (UTC)",
            render: (_, r) =>
              r.lastUsedAt ? r.lastUsedAt.replace("T", " ").replace("Z", "") : "never",
          },
        ]}
      />
      <LoadMore hasMore={hasMore} loading={loadingMore} onMore={onMore} />
    </>
  );
}
