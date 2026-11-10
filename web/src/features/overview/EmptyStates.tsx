import { Alert, Button, Card, Empty, Typography } from "antd";
import { Link } from "react-router-dom";
import { formatTokens, formatUsd, isPositiveMicro } from "../../lib/money";
import type { SavingsHero } from "../../contract/generated";

/** No call has ever reached the workspace: the next step is Setup, not a page of zeros. */
export function NoCalls() {
  return (
    <Card>
      <Empty
        description={
          <>
            <Typography.Text strong>Waiting for your first call</Typography.Text>
            <br />
            <Typography.Text type="secondary">
              Point an agent at your workspace URL and your savings show up here.
            </Typography.Text>
          </>
        }
      >
        <Link to="/setup">
          <Button type="primary">Go to Setup</Button>
        </Link>
      </Empty>
    </Card>
  );
}

/** Calls exist but no monitor has acted: say so, and show what observe mode has found so far. */
export function Collecting({ hero }: { hero: SavingsHero }) {
  return (
    <Alert
      type="info"
      showIcon
      title="Collecting: your monitors are observing"
      description={
        isPositiveMicro(hero.wouldSaveUsd)
          ? `Nothing has been saved yet. Observe mode found ${formatUsd(hero.wouldSaveUsd)} (about ${formatTokens(hero.wouldSaveTokens)} tokens) it would have saved if enabled.`
          : "Nothing has been saved yet. Monitors are watching your traffic and will show what they find here."
      }
    />
  );
}
