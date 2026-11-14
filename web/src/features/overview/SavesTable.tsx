import { Card, Empty, Table, Tag, Typography } from "antd";
import { Link } from "react-router-dom";
import { sessionPath } from "../../api/sessions";
import type { SaveRow } from "../../contract/generated";
import { formatTokens, formatUsd } from "../../lib/money";
import { LoadMore } from "../../shell/LoadMore";
import { monitorLabel } from "../monitors/monitorMeta";
import { ConfidenceTag } from "./ConfidenceTag";

interface Props {
  rows: SaveRow[];
  hasMore: boolean;
  loadingMore: boolean;
  onMore: () => void;
}

/**
 * Biggest saves. Each row is a real monitor event: it links to its session step by event id (never
 * by call index, which restarts), and the row is tagged certain or likely, never summed.
 */
export function SavesTable({ rows, hasMore, loadingMore, onMore }: Props) {
  return (
    <Card title="Biggest saves" aria-label="Biggest saves">
      <Table<SaveRow>
        rowKey="eventId"
        size="small"
        pagination={false}
        dataSource={rows}
        locale={{ emptyText: <Empty description="No saves in this range yet" /> }}
        columns={[
          {
            title: "Saved",
            render: (_, r) => (
              <>
                <Typography.Text strong>{formatUsd(r.estSavedUsd)}</Typography.Text>{" "}
                <ConfidenceTag confidence={r.confidence} />
              </>
            ),
          },
          { title: "Monitor", render: (_, r) => monitorLabel(r.monitor) },
          { title: "Action", render: (_, r) => <Tag>{r.action}</Tag> },
          { title: "Tokens", render: (_, r) => formatTokens(r.estSavedTokens) },
          { title: "Client", render: (_, r) => r.client ?? "unknown" },
          { title: "Model", dataIndex: "model" },
          { title: "When (UTC)", render: (_, r) => r.ts.replace("T", " ").replace("Z", "") },
          {
            title: "Step",
            render: (_, r) => (
              <Link to={sessionPath(r.sessionId, { event: r.eventId })}>
                {r.callIndex > 0 ? `Open call #${r.callIndex}` : "Open step"}
              </Link>
            ),
          },
        ]}
      />
      <LoadMore hasMore={hasMore} loading={loadingMore} onMore={onMore} />
    </Card>
  );
}
