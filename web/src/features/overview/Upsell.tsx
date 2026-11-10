import { Alert, Button, Flex, Typography } from "antd";
import { Link } from "react-router-dom";
import type { UpsellView } from "../../contract/generated";
import { formatTokens, formatUsd } from "../../lib/money";
import { monitorLabel } from "../monitors/monitorMeta";

/** Observe-mode monitors that found savings they did not take. One callout per monitor, largest first. */
export function Upsell({ items }: { items: UpsellView[] }) {
  if (items.length === 0) return null;
  return (
    <Flex vertical gap="small" aria-label="Savings you could add">
      {items.map((u) => (
        <Alert
          key={u.monitor}
          type="warning"
          showIcon
          title={`${monitorLabel(u.monitor)} would save ${formatUsd(u.wouldSaveUsd)}`}
          description={
            <Typography.Text>
              It fired {u.fires} {u.fires === 1 ? "time" : "times"} in observe mode, about{" "}
              {formatTokens(u.wouldSaveTokens)} tokens. Nothing was changed, so this is not in your
              saved figure.
            </Typography.Text>
          }
          action={
            <Link to="/monitors">
              <Button size="small">Open Monitors</Button>
            </Link>
          }
        />
      ))}
    </Flex>
  );
}
