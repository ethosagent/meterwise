import { Flex, Tag, Typography, theme } from "antd";
import { Link } from "react-router-dom";
import type { MemoryHitView } from "../../contract/generated";
import { formatUsd } from "../../lib/money";

/** A hint memory made for one call: the text, how close the match was, the session it came from and the likely saving. */
export function MemoryHintEvent({ hit }: { hit: MemoryHitView }) {
  const { token } = theme.useToken();
  return (
    <Flex vertical gap={token.marginXXS} aria-label="Remembered">
      <Flex gap={token.marginXS} align="center" wrap>
        <Tag>{hit.injected ? "Remembered" : "Would remember"}</Tag>
        <Typography.Text type="secondary">{`${(hit.similarityBp / 100).toFixed(0)}% similar`}</Typography.Text>
        <Link to={`/sessions/${encodeURIComponent(hit.sourceSessionId)}`}>source session</Link>
        <Typography.Text type="secondary">{`likely saved ${formatUsd(hit.estSavedUsd)}`}</Typography.Text>
      </Flex>
      <Typography.Text>{hit.hint}</Typography.Text>
    </Flex>
  );
}
