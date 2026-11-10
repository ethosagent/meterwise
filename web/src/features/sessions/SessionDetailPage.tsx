import { Alert, Breadcrumb, Card, Col, Flex, Row, Skeleton, Tag, Typography, theme } from "antd";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useSessionDetail } from "../../api/sessions";
import { useToastOnError } from "../../api/useToastOnError";
import type { SessionDetail } from "../../contract/generated";
import { formatUsd } from "../../lib/money";
import { WorkspaceGuard } from "../../shell/WorkspaceGuard";
import { RightRail } from "./RightRail";
import { StepList } from "./StepList";
import { duration, highlightedCallId } from "./stepModel";

function Header({ detail }: { detail: SessionDetail }) {
  const { token } = theme.useToken();
  const s = detail.session;
  const models = [...new Set(detail.calls.map((c) => c.model))];
  const stat = (label: string, value: string) => (
    <div>
      <Typography.Text type="secondary">{label}</Typography.Text>
      <br />
      <Typography.Text strong>{value}</Typography.Text>
    </div>
  );
  return (
    <Card aria-label="Session header">
      <Flex gap={token.marginXL} wrap align="flex-end">
        {stat("Client", s.client ?? "unknown")}
        {stat("Model", models.length > 0 ? models.join(", ") : "none yet")}
        {stat("Duration", duration(s.startedAt, s.lastSeenAt))}
        {stat("Calls", String(s.calls))}
        {stat("Spent", s.costUsd === null ? "unpriced" : formatUsd(s.costUsd))}
        {stat("Saved", formatUsd(s.savedUsd))}
        {stat("Likely saved", formatUsd(s.likelySavedUsd))}
        <Tag>{s.outcome}</Tag>
      </Flex>
    </Card>
  );
}

/**
 * `/sessions/:sessionId?event=<eventId>|call=<callId>` (session:read). Deep links name the call or the
 * event by id, never by call index: the step they point at is highlighted.
 */
export function SessionDetailPage() {
  const { token } = theme.useToken();
  const { sessionId = "" } = useParams();
  const [params] = useSearchParams();
  return (
    <section aria-labelledby="page-title">
      <Breadcrumb
        style={{ marginBottom: token.marginXS }}
        items={[{ title: <Link to="/sessions">Sessions</Link> }, { title: sessionId }]}
      />
      <Typography.Title id="page-title" level={2} style={{ marginTop: 0 }}>
        Session
      </Typography.Title>
      <WorkspaceGuard permission="session:read" what="sessions">
        {(ws) => (
          <Body
            workspaceId={ws.id}
            sessionId={sessionId}
            event={params.get("event")}
            call={params.get("call")}
          />
        )}
      </WorkspaceGuard>
    </section>
  );
}

function Body({
  workspaceId,
  sessionId,
  event,
  call,
}: {
  workspaceId: string;
  sessionId: string;
  event: string | null;
  call: string | null;
}) {
  const { token } = theme.useToken();
  const q = useSessionDetail(workspaceId, sessionId, true);
  useToastOnError(q.error, "Could not load this session.");
  if (q.isError) return <Alert type="error" showIcon title="Could not load this session." />;
  if (!q.data) return <Skeleton active />;
  const detail = q.data;
  const target = highlightedCallId(detail, event, call);
  return (
    <Flex vertical gap={token.marginMD}>
      <Header detail={detail} />
      {detail.truncated ? (
        <Alert type="info" showIcon title="This session has more calls than are shown here." />
      ) : null}
      <Row gutter={[token.marginMD, token.marginMD]}>
        <Col xs={24} lg={16}>
          <StepList
            detail={detail}
            workspaceId={workspaceId}
            highlightedCall={target}
            highlightedEvent={
              event && detail.monitorEvents.some((e) => e.id === event) ? event : null
            }
          />
        </Col>
        <Col xs={24} lg={8}>
          <RightRail detail={detail} />
        </Col>
      </Row>
    </Flex>
  );
}
