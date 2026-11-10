import { Card, Flex, Tag, Typography, theme } from "antd";
import { Link } from "react-router-dom";
import type { Call, MonitorEvent } from "../../contract/generated";
import { formatTokens, formatUsd } from "../../lib/money";
import { monitorLabel } from "../monitors/monitorMeta";
import { ConfidenceTag } from "../overview/ConfidenceTag";
import { FeedbackButtons } from "./FeedbackButtons";
import { stepRef } from "./stepModel";

/** What the money on an event means: observe found it, a note or block took it (certain or likely). */
function savedLabel(e: MonitorEvent): string {
  if (e.action === "observe") return "Would have saved";
  return e.confidence === "likely" ? "Likely saved" : "Saved";
}

function MatchedSteps({ indexes, calls }: { indexes: number[]; calls: readonly Call[] }) {
  return (
    <Typography.Text>
      Matched{" "}
      {[...new Set(indexes)].map((n, i) => {
        const ref = stepRef(n, calls);
        return (
          <span key={n}>
            {i > 0 ? ", " : ""}
            {ref.callId ? (
              <Link to={{ search: `?call=${encodeURIComponent(ref.callId)}` }}>{ref.label}</Link>
            ) : (
              ref.label
            )}
          </span>
        );
      })}
    </Typography.Text>
  );
}

/**
 * The evidence for one fired step: which earlier steps matched, the tool, the stored path or its hash
 * prefix, the count against the threshold, the money and its class, and the feedback buttons. A block is
 * worded "answered by the proxy": nothing is replayed and no response body is stored (ruling S10).
 */
export function EvidenceCard({
  event,
  calls,
  workspaceId,
  sessionId,
  highlighted,
}: {
  event: MonitorEvent;
  calls: readonly Call[];
  workspaceId: string;
  sessionId: string;
  highlighted: boolean;
}) {
  const { token } = theme.useToken();
  const ev = event.evidence;
  const matched = ev.matchedCalls ?? [];
  const blockRef = event.action === "block" ? stepRef(matched[0] ?? 0, calls) : null;
  return (
    <Card
      size="small"
      data-testid={`evidence-${event.id}`}
      aria-label={`${monitorLabel(event.monitor)} evidence`}
      aria-current={highlighted ? "true" : undefined}
      style={{
        marginTop: token.marginXS,
        borderColor: highlighted ? token.colorPrimary : undefined,
      }}
    >
      <Flex vertical gap={token.marginXXS}>
        <Flex gap={token.marginXS} align="center" wrap>
          <Typography.Text strong>{monitorLabel(event.monitor)}</Typography.Text>
          <Tag>{event.action}</Tag>
          <ConfidenceTag confidence={event.confidence} />
          <Typography.Text>
            {savedLabel(event)} {formatUsd(event.estSavedUsd)} ({formatTokens(event.estSavedTokens)}{" "}
            tokens)
          </Typography.Text>
        </Flex>
        {blockRef ? (
          <Typography.Text type="secondary">
            Answered by the proxy: pointed the agent back to{" "}
            {blockRef.callId ? `${blockRef.label}'s` : "an earlier call's"} result.
          </Typography.Text>
        ) : null}
        {matched.length > 0 ? <MatchedSteps indexes={matched} calls={calls} /> : null}
        {ev.tool ? <Typography.Text>Tool {ev.tool}</Typography.Text> : null}
        {ev.path ? <Typography.Text>Path {ev.path}</Typography.Text> : null}
        {!ev.path && ev.pathHash ? (
          <Typography.Text>Path hash {ev.pathHash.slice(0, 8)}</Typography.Text>
        ) : null}
        {ev.count ? (
          <Typography.Text>
            Seen {ev.count} times{ev.threshold ? ` (threshold ${ev.threshold})` : ""}
          </Typography.Text>
        ) : null}
        {ev.phrases && ev.phrases.length > 0 ? (
          <Typography.Text>Phrases: {ev.phrases.join(", ")}</Typography.Text>
        ) : null}
        <FeedbackButtons
          workspaceId={workspaceId}
          sessionId={sessionId}
          eventId={event.id}
          value={event.feedback}
        />
      </Flex>
    </Card>
  );
}
