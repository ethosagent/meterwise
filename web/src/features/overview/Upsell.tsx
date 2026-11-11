import { Alert, Button, Flex, Typography } from "antd";
import { Link } from "react-router-dom";
import type { UpsellView } from "../../contract/generated";
import { formatTokens, formatUsd } from "../../lib/money";
import { causeLabel } from "./causeMeta";

/** Observe-mode causes (monitors, trimming, routing, memory) that found savings they did not take. One callout per cause, largest first. */
export function Upsell({ items }: { items: UpsellView[] }) {
  if (items.length === 0) return null;
  return (
    <Flex vertical gap="small" aria-label="Savings you could add">
      {items.map((u) => (
        <Alert
          key={u.cause}
          type="warning"
          showIcon
          title={`${causeLabel(u.cause)} would save ${formatUsd(u.wouldSaveUsd)}`}
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
