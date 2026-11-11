import { Alert, Descriptions, Drawer, Skeleton, Table, Tag, Typography } from "antd";
import { useTrimPreview } from "../../api/trim";
import type { TrimStageView } from "../../contract/generated";
import { formatTokens } from "../../lib/money";
import { stageLabel } from "./trimMeta";

const COLUMNS = [
  { title: "Stage", key: "stage", render: (_: unknown, r: TrimStageView) => stageLabel(r.stage) },
  { title: "Tool", key: "tool", render: (_: unknown, r: TrimStageView) => r.tool ?? "several" },
  { title: "Regions", dataIndex: "regions", key: "regions", align: "right" as const },
  {
    title: "Bytes",
    key: "bytes",
    align: "right" as const,
    render: (_: unknown, r: TrimStageView) =>
      `${formatTokens(r.bytesBefore)} → ${formatTokens(r.bytesAfter)}`,
  },
  {
    title: "Tokens",
    key: "tokens",
    align: "right" as const,
    render: (_: unknown, r: TrimStageView) => `−${formatTokens(r.estTokens)}`,
  },
  {
    title: "Marker",
    key: "marker",
    render: (_: unknown, r: TrimStageView) =>
      r.reverted ? (
        <Tag>reverted: not enough gain</Tag>
      ) : (
        <Typography.Text code>{r.marker}</Typography.Text>
      ),
  },
];

/**
 * What trimming did to one call, stage by stage: sizes and the fixed marker each stage writes. The request text is
 * never stored, so there is nothing else to show.
 */
export function TrimPreviewDrawer({
  workspaceId,
  callId,
  onClose,
}: {
  workspaceId: string;
  callId: string;
  onClose: () => void;
}) {
  const q = useTrimPreview(workspaceId, callId, true);
  const p = q.data;
  return (
    <Drawer open onClose={onClose} title="What trimming did to this call" size="large">
      {q.isError ? <Alert type="error" showIcon title="Could not load the trim details." /> : null}
      {q.isPending ? <Skeleton active /> : null}
      {p ? (
        <>
          <Typography.Paragraph type="secondary">
            Sizes and our markers only. No request text is stored, so the trimmed content is not
            shown.
          </Typography.Paragraph>
          <Descriptions size="small" column={1} style={{ marginBottom: 16 }}>
            <Descriptions.Item label="Outcome">
              {p.action === "observe"
                ? "Observed: nothing was changed"
                : p.action === "acted"
                  ? "Applied"
                  : "Not trimmed"}
            </Descriptions.Item>
            <Descriptions.Item label="Trimmed text (all stages)">
              {`${formatTokens(p.bodyBefore)} → ${formatTokens(p.bodyAfter)} bytes`}
            </Descriptions.Item>
          </Descriptions>
          <Table<TrimStageView>
            size="small"
            pagination={false}
            rowKey={(r) => r.stage}
            dataSource={p.stages}
            columns={COLUMNS}
            locale={{ emptyText: "No stage fired on this call." }}
          />
        </>
      ) : null}
    </Drawer>
  );
}
