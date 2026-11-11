import { Flex, Tag, Typography, theme } from "antd";
import type { Call } from "../../contract/generated";
import { formatTokens, formatUsd } from "../../lib/money";

/** What trimming did to one call: how many tokens, and whether the saving is certain or likely. An observe is only a would-save. */
export function TrimEvent({ call }: { call: Call }) {
  const { token } = theme.useToken();
  const observed = call.trimAction === "observe";
  return (
    <Flex gap={token.marginXS} align="center" wrap aria-label="Trim">
      <Tag>{observed ? "Would trim" : "Trimmed"}</Tag>
      <Typography.Text>{`−${formatTokens(call.trimTokens)} tokens`}</Typography.Text>
      <Tag color={!observed && call.trimConfidence === "certain" ? "success" : "default"}>
        {observed ? "would save" : (call.trimConfidence ?? "unknown")}
      </Tag>
      <Typography.Text type="secondary">{formatUsd(call.savedUsdTrim)}</Typography.Text>
    </Flex>
  );
}
