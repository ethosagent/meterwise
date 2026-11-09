import { Badge, Typography } from "antd";
import type { SetupStatus } from "../../api/contract04";
import { relativeTime } from "../../api/relative";

/** "waiting for first call…" until the workspace has traffic, then "connected · last call 2 minutes ago · 5 calls today". */
export function Status({ status }: { status: SetupStatus }) {
  const connected = status.lastCallAt !== null;
  return (
    <Typography.Text>
      <Badge status={connected ? "success" : "processing"} />{" "}
      {connected && status.lastCallAt !== null
        ? `connected · last call ${relativeTime(status.lastCallAt)} · ${status.callsToday} ${status.callsToday === 1 ? "call" : "calls"} today`
        : "waiting for first call…"}
    </Typography.Text>
  );
}
