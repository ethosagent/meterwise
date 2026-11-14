import { Alert, Card, Divider, Flex, Skeleton, theme } from "antd";
import { useMemoryConfig } from "../../api/memory";
import { usePatchRouter, useRouterConfig } from "../../api/router";
import { useTrimConfig } from "../../api/trim";
import { useErrorToast } from "../../api/useErrorToast";
import { useToastOnError } from "../../api/useToastOnError";
import { MemoryRow } from "../memory/MemoryRow";
import { RouterPanel } from "../router/RouterPanel";
import { TrimSettings } from "../trim/TrimSettings";
import { type ModeConfirm, type ModeOption, ModeSegment } from "./ModeSegment";
import { SettingsRow } from "./SettingsRow";

const ROUTER_MODES: ModeOption[] = [
  { value: "off", label: "Off" },
  { value: "observe", label: "Observe" },
  { value: "conservative", label: "Conservative" },
  { value: "aggressive", label: "Aggressive" },
];
const ROUTER_ACTING = (name: string): ModeConfirm => ({
  title: `Turn on ${name} routing?`,
  description:
    "Eligible calls are sent to a cheaper model than the one the agent asked for. Monitors watch for regressions.",
  okText: name,
});
const ROUTER_CONFIRM: Record<string, ModeConfirm> = {
  conservative: ROUTER_ACTING("Conservative"),
  aggressive: ROUTER_ACTING("Aggressive"),
};
/**
 * The three savers beyond the monitors, one row each: trimming, model routing and memory. Each mode is sent
 * through its own PATCH and refreshes the savings figures. Routing's acting modes and memory's hint wait behind a
 * confirm. The figures and finer settings of each saver arrive with its feature.
 */
export function SaversCard({
  workspaceId,
  readOnly,
  canStats,
}: {
  workspaceId: string;
  readOnly: boolean;
  canStats: boolean;
}) {
  const { token } = theme.useToken();
  const toast = useErrorToast();
  const trim = useTrimConfig(workspaceId, true);
  const router = useRouterConfig(workspaceId, true);
  const memory = useMemoryConfig(workspaceId, true);
  const patchRouter = usePatchRouter(workspaceId);
  useToastOnError(trim.error, "Could not load the trim settings.");
  useToastOnError(router.error, "Could not load the router settings.");
  useToastOnError(memory.error, "Could not load the memory settings.");

  if (trim.isError || router.isError || memory.isError) {
    return <Alert type="error" showIcon title="Could not load the saver settings." />;
  }
  if (!trim.data || !router.data || !memory.data) return <Skeleton active />;
  const fail = (what: string) => ({ onError: (e: Error) => toast(e, `Could not update ${what}.`) });
  return (
    <Card title="Savers" aria-label="Savers">
      <Flex vertical gap={token.marginMD}>
        <TrimSettings workspaceId={workspaceId} readOnly={readOnly} />
        <Divider style={{ margin: 0 }} />
        <SettingsRow
          title="Model routing"
          about="Sends easy calls to a cheaper model of the same provider. Observe records what it would have saved."
        >
          <ModeSegment
            label="Model routing mode"
            value={router.data.config.mode}
            options={ROUTER_MODES}
            confirm={ROUTER_CONFIRM}
            readOnly={readOnly}
            busy={patchRouter.isPending}
            onChange={(mode) => patchRouter.mutate({ mode }, fail("model routing"))}
          />
        </SettingsRow>
        <RouterPanel
          workspaceId={workspaceId}
          settings={router.data}
          readOnly={readOnly}
          canStats={canStats}
        />
        <Divider style={{ margin: 0 }} />
        <MemoryRow workspaceId={workspaceId} readOnly={readOnly} canStats={canStats} />
      </Flex>
    </Card>
  );
}
