import { Card, Flex, Tag, Typography, theme } from "antd";
import { useEffect, useRef } from "react";
import type { Call, SessionDetail } from "../../contract/generated";
import { formatTokens, formatUsd } from "../../lib/money";
import { EvidenceCard } from "./EvidenceCard";
import { blockEventOf, eventsOf } from "./stepModel";

/** The money of a step: a blocked call costs nothing, so it reads "saved $x" from its block event. */
function StepCost({ call, detail }: { call: Call; detail: SessionDetail }) {
  if (call.blocked) {
    const block = blockEventOf(detail, call.id);
    return (
      <Typography.Text strong>
        {block ? `saved ${formatUsd(block.estSavedUsd)}` : "blocked"}
      </Typography.Text>
    );
  }
  return (
    <Typography.Text>
      {call.costUsd === null ? "unpriced" : formatUsd(call.costUsd)}
    </Typography.Text>
  );
}

function Step({
  call,
  detail,
  workspaceId,
  highlighted,
  highlightedEventId,
}: {
  call: Call;
  detail: SessionDetail;
  workspaceId: string;
  highlighted: boolean;
  highlightedEventId: string | null;
}) {
  const { token } = theme.useToken();
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (highlighted) ref.current?.scrollIntoView?.({ block: "center" });
  }, [highlighted]);
  const events = eventsOf(detail, call.id);
  return (
    <div
      ref={ref}
      id={`step-${call.id}`}
      data-testid={`step-${call.id}`}
      aria-current={highlighted ? "step" : undefined}
    >
      <Card
        size="small"
        style={{
          borderColor: highlighted ? token.colorPrimary : undefined,
          borderWidth: highlighted ? token.lineWidthBold : undefined,
        }}
      >
        <Flex justify="space-between" gap={token.marginSM} wrap>
          <Flex gap={token.marginXS} align="center" wrap>
            <Typography.Text strong>{`#${call.callIndex}`}</Typography.Text>
            <Typography.Text type="secondary">{call.model}</Typography.Text>
            {call.blocked ? <Tag color="success">blocked</Tag> : null}
            {call.notesInjected > 0 ? (
              <Tag>
                {call.notesInjected} {call.notesInjected === 1 ? "note" : "notes"} added
              </Tag>
            ) : null}
            {call.toolCalls.map((t, i) => (
              <Tag key={`${t}:${i}`}>{t}</Tag>
            ))}
          </Flex>
          <Flex gap={token.marginSM} align="center">
            <Typography.Text type="secondary">
              {formatTokens(call.inputTokens)} in / {formatTokens(call.outputTokens)} out
            </Typography.Text>
            <StepCost call={call} detail={detail} />
          </Flex>
        </Flex>
        {events.map((e) => (
          <EvidenceCard
            key={e.id}
            event={e}
            calls={detail.calls}
            workspaceId={workspaceId}
            sessionId={detail.session.id}
            highlighted={
              highlightedEventId === e.id || (highlighted && highlightedEventId === null)
            }
          />
        ))}
      </Card>
    </div>
  );
}

/** The session's calls in order; fired steps carry their evidence cards, the deep-linked step is highlighted. */
export function StepList({
  detail,
  workspaceId,
  highlightedCall,
  highlightedEvent,
}: {
  detail: SessionDetail;
  workspaceId: string;
  highlightedCall: string | null;
  highlightedEvent: string | null;
}) {
  const { token } = theme.useToken();
  return (
    <Flex vertical gap={token.marginSM} aria-label="Steps">
      {detail.calls.map((c) => (
        <Step
          key={c.id}
          call={c}
          detail={detail}
          workspaceId={workspaceId}
          highlighted={c.id === highlightedCall}
          highlightedEventId={highlightedEvent}
        />
      ))}
    </Flex>
  );
}
