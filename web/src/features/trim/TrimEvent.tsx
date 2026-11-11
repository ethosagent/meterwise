import { Button, Flex, Tag, Typography, theme } from "antd";
import { useState } from "react";
import type { Call } from "../../contract/generated";
import { formatTokens, formatUsd } from "../../lib/money";
import { TrimPreviewDrawer } from "./TrimPreviewDrawer";

/**
 * What trimming did to one call: how many tokens, and whether the saving is certain or likely. An observe is only a
 * would-save. With a workspace id, "Details" opens the per-stage sizes and markers.
 */
export function TrimEvent({ call, workspaceId }: { call: Call; workspaceId?: string }) {
  const { token } = theme.useToken();
  const [open, setOpen] = useState(false);
  const observed = call.trimAction === "observe";
  return (
    <Flex gap={token.marginXS} align="center" wrap aria-label="Trim">
      <Tag>{observed ? "Would trim" : "Trimmed"}</Tag>
      <Typography.Text>{`−${formatTokens(call.trimTokens)} tokens`}</Typography.Text>
      <Tag color={!observed && call.trimConfidence === "certain" ? "success" : "default"}>
        {observed ? "would save" : (call.trimConfidence ?? "unknown")}
      </Tag>
      <Typography.Text type="secondary">{formatUsd(call.savedUsdTrim)}</Typography.Text>
      {workspaceId ? (
        <Button size="small" type="link" onClick={() => setOpen(true)}>
          Details
        </Button>
      ) : null}
      {open && workspaceId ? (
        <TrimPreviewDrawer
          workspaceId={workspaceId}
          callId={call.id}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </Flex>
  );
}
