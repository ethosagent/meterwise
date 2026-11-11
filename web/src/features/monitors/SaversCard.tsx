import { Alert, Card, Divider, Flex, Skeleton, theme } from "antd";
import { useMemoryConfig, usePatchMemory } from "../../api/memory";
import { usePatchRouter, useRouterConfig } from "../../api/router";
import { usePatchTrim, useTrimConfig } from "../../api/trim";
import { useErrorToast } from "../../api/useErrorToast";
import { useToastOnError } from "../../api/useToastOnError";
import { type ModeConfirm, type ModeOption, ModeSegment } from "./ModeSegment";
import { SettingsRow } from "./SettingsRow";

const TRIM_MODES: ModeOption[] = [
  { value: "off", label: "Off" },
  { value: "observe", label: "Observe" },
  { value: "lossless", label: "Lossless" },
  { value: "aggressive", label: "Aggressive" },
];
const ROUTER_MODES: ModeOption[] = [
  { value: "off", label: "Off" },
  { value: "observe", label: "Observe" },
  { value: "conservative", label: "Conservative" },
  { value: "aggressive", label: "Aggressive" },
];
const MEMORY_MODES: ModeOption[] = [
  { value: "off", label: "Off" },
  { value: "observe", label: "Observe" },
  { value: "hint", label: "Hint" },
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
const MEMORY_CONFIRM: Record<string, ModeConfirm> = {
  hint: {
    title: "Turn on Memory hints?",
    description:
      "A short hint from a past session is added to a later call of a similar task. The agent sees it.",
    okText: "Hint",
  },
};

const DISABLED_WHY: Record<string, string> = {
  no_key: "Not enabled: no memory model key is configured on this server.",
  privacy: "Not enabled: this workspace does not allow capturing content.",
  budget: "Paused: memory's own daily model budget is spent.",
};

/**
 * The three savers beyond the monitors, one row each: trimming, model routing and memory. Each mode is sent
 * through its own PATCH and refreshes the savings figures. Routing's acting modes and memory's hint wait behind a
 * confirm. The figures and finer settings of each saver arrive with its feature.
 */
export function SaversCard({ workspaceId, readOnly }: { workspaceId: string; readOnly: boolean }) {
  const { token } = theme.useToken();
  const toast = useErrorToast();
  const trim = useTrimConfig(workspaceId, true);
  const router = useRouterConfig(workspaceId, true);
  const memory = useMemoryConfig(workspaceId, true);
  const patchTrim = usePatchTrim(workspaceId);
  const patchRouter = usePatchRouter(workspaceId);
  const patchMemory = usePatchMemory(workspaceId);
  useToastOnError(trim.error, "Could not load the trim settings.");
  useToastOnError(router.error, "Could not load the router settings.");
  useToastOnError(memory.error, "Could not load the memory settings.");

  if (trim.isError || router.isError || memory.isError) {
    return <Alert type="error" showIcon title="Could not load the saver settings." />;
  }
  if (!trim.data || !router.data || !memory.data) return <Skeleton active />;
  const fail = (what: string) => ({ onError: (e: Error) => toast(e, `Could not update ${what}.`) });
  const reason = memory.data.disabledReason;
  return (
    <Card title="Savers" aria-label="Savers">
      <Flex vertical gap={token.marginMD}>
        <SettingsRow
          title="Trimming"
          about="Shrinks bulky tool output (repeated lines, long logs, big JSON) before it is sent. Observe measures without changing anything."
        >
          <ModeSegment
            label="Trimming mode"
            value={trim.data.mode}
            options={TRIM_MODES}
            readOnly={readOnly}
            busy={patchTrim.isPending}
            onChange={(mode) => patchTrim.mutate({ mode }, fail("trimming"))}
          />
        </SettingsRow>
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
        <Divider style={{ margin: 0 }} />
        <SettingsRow
          title="Memory"
          about="Remembers how past sessions solved a task and hints at it when a similar one starts. Savings from memory are always likely, never certain."
          note={memory.data.enabled || !reason ? null : (DISABLED_WHY[reason] ?? reason)}
        >
          <ModeSegment
            label="Memory mode"
            value={memory.data.config.mode}
            options={MEMORY_MODES}
            confirm={MEMORY_CONFIRM}
            readOnly={readOnly}
            busy={patchMemory.isPending}
            onChange={(mode) => patchMemory.mutate({ mode }, fail("memory"))}
          />
        </SettingsRow>
      </Flex>
    </Card>
  );
}
