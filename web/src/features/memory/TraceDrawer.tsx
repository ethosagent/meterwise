import { Alert, Descriptions, Drawer, Flex, Skeleton, Tag, Typography, theme } from "antd";
import { Link } from "react-router-dom";
import { useTrace } from "../../api/memory";
import { useToastOnError } from "../../api/useToastOnError";
import type { TraceDetail } from "../../contract/generated";
import { formatBp } from "../../lib/money";

const sessionLink = (id: string) => <Link to={`/sessions/${encodeURIComponent(id)}`}>{id}</Link>;

/** "Reused N times" and the sessions it helped: the customer sees one fix paying off repeatedly. */
export function ReusedBadge({ trace }: { trace: TraceDetail }) {
  const { token } = theme.useToken();
  return (
    <Flex vertical gap={token.marginXXS} aria-label="Reuse">
      <Tag style={{ alignSelf: "flex-start" }}>
        {`Reused ${trace.uses} ${trace.uses === 1 ? "time" : "times"}`}
      </Tag>
      {trace.helpedSessions.length > 0 ? (
        <Flex gap={token.marginXS} wrap aria-label="Sessions it helped">
          <Typography.Text type="secondary">Helped</Typography.Text>
          {trace.helpedSessions.map((id) => (
            <span key={id}>{sessionLink(id)}</span>
          ))}
        </Flex>
      ) : null}
    </Flex>
  );
}

function Body({ trace }: { trace: TraceDetail }) {
  const { token } = theme.useToken();
  const sig = trace.signature;
  return (
    <Flex vertical gap={token.marginMD}>
      <Typography.Paragraph type="secondary" style={{ margin: 0 }}>
        The distilled solution as memory stores it. It never holds provider keys.
      </Typography.Paragraph>
      <ReusedBadge trace={trace} />
      <Descriptions column={1} size="small" bordered>
        <Descriptions.Item label="Task">{trace.taskSummary}</Descriptions.Item>
        <Descriptions.Item label="Approach">{trace.approach}</Descriptions.Item>
        <Descriptions.Item label="Fix">{trace.fix}</Descriptions.Item>
        <Descriptions.Item label="Observations">
          {trace.observations.length === 0 ? "none" : trace.observations.join(" / ")}
        </Descriptions.Item>
        <Descriptions.Item label="Files">{sig.files.join(", ") || "none"}</Descriptions.Item>
        <Descriptions.Item label="Errors">{sig.errors.join(", ") || "none"}</Descriptions.Item>
        <Descriptions.Item label="Tools">{sig.tools.join(", ") || "none"}</Descriptions.Item>
        <Descriptions.Item label="Repo">{sig.repo ?? "unknown"}</Descriptions.Item>
        <Descriptions.Item label="Confidence">{formatBp(trace.confidenceBp)}</Descriptions.Item>
        <Descriptions.Item label="Ratings">
          {`${trace.upvotes} helped, ${trace.downvotes} irrelevant`}
        </Descriptions.Item>
        <Descriptions.Item label="Came from">{sessionLink(trace.sessionId)}</Descriptions.Item>
      </Descriptions>
    </Flex>
  );
}

/** One trace in full, with the session it came from and the sessions it helped (session:read). */
export function TraceDrawer({
  workspaceId,
  traceId,
  onClose,
}: {
  workspaceId: string;
  traceId: string;
  onClose: () => void;
}) {
  const q = useTrace(workspaceId, traceId, true);
  useToastOnError(q.error, "Could not load this trace.");
  return (
    <Drawer open onClose={onClose} title="Remembered solution" size="large">
      {q.isError ? <Alert type="error" showIcon title="Could not load this trace." /> : null}
      {q.isPending ? <Skeleton active /> : null}
      {q.data ? <Body trace={q.data} /> : null}
    </Drawer>
  );
}
