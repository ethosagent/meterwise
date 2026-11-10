import { Alert, Flex, Skeleton, theme } from "antd";
import { useMemo } from "react";
import type { RangeSel } from "../../api/range";
import { useSaves, useSavingsOverview } from "../../api/savings";
import { useToastOnError } from "../../api/useToastOnError";
import { CauseBars } from "./CauseBars";
import { Collecting, NoCalls } from "./EmptyStates";
import { Hero } from "./Hero";
import { HowWeCount } from "./HowWeCount";
import { SavesTable } from "./SavesTable";
import { SpendChart } from "./SpendChart";
import { Upsell } from "./Upsell";

/** The Summary tab: hero, spend chart, causes, biggest saves, upsell and "How we count". */
export function SummaryTab({ workspaceId, sel }: { workspaceId: string; sel: RangeSel }) {
  const { token } = theme.useToken();
  const overview = useSavingsOverview(workspaceId, sel, true);
  const data = overview.data;
  // Asked in parallel with the overview (no waterfall); the table renders only once something acted.
  const saves = useSaves(workspaceId, sel, true);
  const rows = useMemo(() => saves.data?.pages.flatMap((p) => p.items) ?? [], [saves.data]);
  useToastOnError(overview.error, "Could not load your savings.");
  useToastOnError(saves.error, "Could not load the biggest saves.");

  if (overview.isError) return <Alert type="error" showIcon title="Could not load your savings." />;
  if (!data) return <Skeleton active />;
  if (data.state === "no_calls") return <NoCalls />;
  return (
    <Flex vertical gap={token.marginLG}>
      {data.state === "collecting" ? <Collecting hero={data.hero} /> : null}
      <Hero hero={data.hero} />
      <SpendChart days={data.days} />
      <CauseBars causes={data.byCause} />
      <Upsell items={data.upsell} />
      {data.state === "active" ? (
        <SavesTable
          rows={rows}
          hasMore={saves.hasNextPage}
          loadingMore={saves.isFetchingNextPage}
          onMore={() => void saves.fetchNextPage()}
        />
      ) : null}
      <HowWeCount />
    </Flex>
  );
}
