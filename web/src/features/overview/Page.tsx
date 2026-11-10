import { Flex, Tabs, theme } from "antd";
import { useSearchParams } from "react-router-dom";
import { useRangeParams } from "../../api/range";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { RangeControl } from "../../shell/RangeControl";
import { WorkspaceGuard } from "../../shell/WorkspaceGuard";
import { CausesTab } from "./CausesTab";
import { SummaryTab } from "./SummaryTab";

const CAUSES = "causes";

/**
 * Overview (savings:read): Summary and Where it came from. The range and the tab live in the URL
 * (`?range=&from=&to=&tab=causes`), so a reload or a shared link reproduces the view.
 */
export function OverviewPage() {
  const { token } = theme.useToken();
  const { sel, setRange } = useRangeParams();
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === CAUSES ? CAUSES : "summary";
  return (
    <section aria-labelledby="page-title">
      <PageHead page={pageByKey("overview")} />
      <WorkspaceGuard permission="savings:read" what="savings">
        {(ws) => (
          <Flex vertical gap={token.marginMD}>
            <RangeControl value={sel} onChange={setRange} />
            <Tabs
              activeKey={tab}
              onChange={(key) =>
                setParams(
                  (prev) => {
                    const q = new URLSearchParams(prev);
                    if (key === CAUSES) q.set("tab", CAUSES);
                    else q.delete("tab");
                    return q;
                  },
                  { replace: true },
                )
              }
              items={[
                {
                  key: "summary",
                  label: "Summary",
                  children: tab === "summary" ? <SummaryTab workspaceId={ws.id} sel={sel} /> : null,
                },
                {
                  key: CAUSES,
                  label: <span>Where it came from</span>,
                  children: tab === CAUSES ? <CausesTab workspaceId={ws.id} sel={sel} /> : null,
                },
              ]}
            />
          </Flex>
        )}
      </WorkspaceGuard>
    </section>
  );
}
