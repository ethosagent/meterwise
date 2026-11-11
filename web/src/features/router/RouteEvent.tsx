import { DislikeFilled, DislikeOutlined } from "@ant-design/icons";
import { Button, Flex, Tag, Typography, theme } from "antd";
import { useState } from "react";
import { useRouterFeedback } from "../../api/router";
import { useErrorToast } from "../../api/useErrorToast";
import type { Call } from "../../contract/generated";
import { formatUsd } from "../../lib/money";

/**
 * What the router did to one call: the model it was sent to, the score and the money against the model asked for.
 * A routed call (not an observe) carries a thumbs-down: "this answer was worse than the model I asked for".
 * Clicking it again clears the verdict. The call has no stored verdict field, so the pressed state is what the last
 * successful POST returned.
 */
export function RouteEvent({
  call,
  workspaceId,
  sessionId,
}: {
  call: Call;
  workspaceId: string;
  sessionId: string;
}) {
  const { token } = theme.useToken();
  const toast = useErrorToast();
  const mutation = useRouterFeedback(workspaceId, sessionId);
  const [verdict, setVerdict] = useState<string | null>(null);
  const observed = call.routerAction === "observe";
  const worse = verdict === "worse";
  const send = (next: "worse" | null) =>
    mutation.mutate(
      { callId: call.id, verdict: next },
      {
        onSuccess: (view) => setVerdict(view.verdict),
        onError: (e) => toast(e, "Could not save your feedback."),
      },
    );
  return (
    <Flex gap={token.marginXS} align="center" wrap aria-label="Route">
      <Tag color={observed ? "default" : "success"}>{observed ? "Would route" : "Routed"}</Tag>
      <Typography.Text>{`${call.routedFrom ?? call.model} → ${call.routedTo ?? "?"}`}</Typography.Text>
      <Typography.Text type="secondary">
        {`${observed ? "would save" : "saved"} ${formatUsd(call.savedUsdRouter)} vs ${call.routedFrom ?? call.model}`}
      </Typography.Text>
      {call.routerScoreBp === null ? null : (
        <Typography.Text type="secondary">{`score ${(call.routerScoreBp / 100).toFixed(0)}`}</Typography.Text>
      )}
      {observed ? null : (
        <Button
          size="small"
          aria-label="Mark routed answer worse"
          aria-pressed={worse}
          disabled={mutation.isPending}
          icon={worse ? <DislikeFilled /> : <DislikeOutlined />}
          onClick={() => send(worse ? null : "worse")}
        />
      )}
    </Flex>
  );
}
