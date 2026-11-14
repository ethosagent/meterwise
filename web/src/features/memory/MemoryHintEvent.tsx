import { DislikeFilled, DislikeOutlined, LikeFilled, LikeOutlined } from "@ant-design/icons";
import { Button, Flex, Space, Tag, Typography, theme } from "antd";
import { Link } from "react-router-dom";
import { type HintFeedback, useMemoryHitFeedback } from "../../api/memory";
import { useErrorToast } from "../../api/useErrorToast";
import type { MemoryHitView } from "../../contract/generated";
import { formatBp, formatUsd, isPositiveMicro } from "../../lib/money";

/** 👍 helped / 👎 irrelevant on one hint. Clicking the pressed one clears it; the stored value is what shows. */
function HintFeedbackButtons({
  hit,
  workspaceId,
  sessionId,
}: {
  hit: MemoryHitView;
  workspaceId: string;
  sessionId: string;
}) {
  const toast = useErrorToast();
  const mutation = useMemoryHitFeedback(workspaceId, sessionId);
  const send = (feedback: HintFeedback) =>
    mutation.mutate(
      { hitId: hit.id, feedback },
      { onError: (e) => toast(e, "Could not save your feedback.") },
    );
  const helpful = hit.feedback === "helpful";
  const irrelevant = hit.feedback === "irrelevant";
  return (
    <Space size="small" role="group" aria-label="Was this hint useful?">
      <Button
        size="small"
        aria-label="Hint helped"
        aria-pressed={helpful}
        disabled={mutation.isPending}
        icon={helpful ? <LikeFilled /> : <LikeOutlined />}
        onClick={() => send(helpful ? null : "helpful")}
      />
      <Button
        size="small"
        aria-label="Hint was irrelevant"
        aria-pressed={irrelevant}
        disabled={mutation.isPending}
        icon={irrelevant ? <DislikeFilled /> : <DislikeOutlined />}
        onClick={() => send(irrelevant ? null : "irrelevant")}
      />
    </Space>
  );
}

/**
 * A hint memory made for one call: the exact text the agent got (or, in observe, would have got), how close the match
 * was, the session it came from, the likely saving (grey, never green) and the feedback buttons.
 */
export function MemoryHintEvent({
  hit,
  workspaceId,
  sessionId,
}: {
  hit: MemoryHitView;
  workspaceId: string;
  sessionId: string;
}) {
  const { token } = theme.useToken();
  return (
    <Flex vertical gap={token.marginXXS} aria-label="Remembered">
      <Flex gap={token.marginXS} align="center" wrap>
        <Tag>{hit.injected ? "Remembered" : "Would have hinted"}</Tag>
        <Typography.Text type="secondary">{`${formatBp(hit.similarityBp)} similar`}</Typography.Text>
        <Link to={`/sessions/${encodeURIComponent(hit.sourceSessionId)}`}>source session</Link>
        {isPositiveMicro(hit.estSavedUsd) ? (
          <Typography.Text type="secondary">
            {`${hit.injected ? "likely saved" : "would likely save"} ${formatUsd(hit.estSavedUsd)} compared with similar sessions without a hint`}
          </Typography.Text>
        ) : null}
        <HintFeedbackButtons hit={hit} workspaceId={workspaceId} sessionId={sessionId} />
      </Flex>
      <Typography.Text>{hit.hint}</Typography.Text>
    </Flex>
  );
}
