import { Flex, Tag, Typography, theme } from "antd";
import type { Call } from "../../contract/generated";
import { formatUsd } from "../../lib/money";

/** What the router did to one call: the model it was sent to, the score and the money against the model asked for. */
export function RouteEvent({ call }: { call: Call }) {
  const { token } = theme.useToken();
  const observed = call.routerAction === "observe";
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
    </Flex>
  );
}
