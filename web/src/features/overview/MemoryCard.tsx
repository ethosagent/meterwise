import { Card, Flex, Skeleton, Statistic, Tag, Typography, theme } from "antd";
import { Link } from "react-router-dom";
import { useMemoryConfig, useMemoryStats } from "../../api/memory";
import type { RangeSel } from "../../api/range";
import { useToastOnError } from "../../api/useToastOnError";
import { usePermission } from "../../auth/usePermission";
import type { MemoryStats } from "../../contract/generated";
import { formatBp, formatUsd } from "../../lib/money";
import { memoryStateText } from "../memory/memoryText";
import { useFirstHintToast } from "../memory/useFirstHint";

/** The share of rated hints that helped, as basis points; null while nothing is rated. */
function helpfulBp(s: MemoryStats): number | null {
  const rated = s.helpful + s.irrelevant;
  return rated === 0 ? null : Math.floor((s.helpful * 10000) / rated);
}

function Figures({ stats }: { stats: MemoryStats }) {
  const { token } = theme.useToken();
  const rate = helpfulBp(stats);
  return (
    <Flex gap={token.marginLG} wrap aria-label="Memory figures">
      <Statistic title="Traces stored" value={stats.traces} />
      <Statistic title="Sessions hinted" value={stats.hintedSessions} />
      <Statistic
        title={
          <span>
            Likely saved <Tag style={{ color: token.colorTextSecondary }}>likely</Tag>
          </span>
        }
        value={formatUsd(stats.likelySavedUsd)}
        styles={{ content: { color: token.colorTextSecondary } }}
      />
      <Statistic
        title="Hints rated helpful"
        value={rate === null ? "not rated yet" : formatBp(rate)}
      />
    </Flex>
  );
}

/**
 * The Memory card on Overview: what memory stored and hinted in the range, the likely saving (always grey, never added
 * to the saved figure), and the state it is in when there is nothing to count yet. The first injected hint shows a
 * one-time toast.
 */
export function MemoryCard({ workspaceId, sel }: { workspaceId: string; sel: RangeSel }) {
  const canConfig = usePermission("monitor_config:read", workspaceId);
  const config = useMemoryConfig(workspaceId, canConfig);
  const stats = useMemoryStats(workspaceId, sel, true);
  useToastOnError(stats.error, "Could not load the memory figures.");
  useToastOnError(config.error, "Could not load the memory settings.");
  const s = stats.data;
  useFirstHintToast(workspaceId, s ? s.hits - s.observedHits : undefined);
  if (stats.isError) return null;
  if (!s) return <Skeleton active paragraph={{ rows: 1 }} />;
  const state = config.data ? memoryStateText(config.data, s) : null;
  const observing = s.mode === "observe" && s.observedHits > 0;
  return (
    <Card
      title="Memory"
      aria-label="Memory"
      extra={<Link to="/settings/memory">Remembered solutions</Link>}
    >
      <Flex vertical gap="small">
        {state ? <Typography.Text>{state}</Typography.Text> : null}
        {config.data?.enabled === false ? null : <Figures stats={s} />}
        {observing ? (
          <Typography.Text type="secondary">
            {`memory is observing — would have hinted ${s.observedHits} ${s.observedHits === 1 ? "time" : "times"}. Nothing was changed.`}
          </Typography.Text>
        ) : null}
        <Typography.Text type="secondary">
          Memory savings are likely, never certain, and are not part of the saved figure.
        </Typography.Text>
      </Flex>
    </Card>
  );
}
