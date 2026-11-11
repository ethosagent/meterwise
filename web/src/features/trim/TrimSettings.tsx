import { Button, Flex, Typography, theme } from "antd";
import { useState } from "react";
import type { RangeSel } from "../../api/range";
import { usePatchTrim, useTrimConfig, useTrimStats } from "../../api/trim";
import { useErrorToast } from "../../api/useErrorToast";
import { formatTokens, formatUsd, parseMicro } from "../../lib/money";
import { type ModeConfirm, type ModeOption, ModeSegment } from "../monitors/ModeSegment";
import { SettingsRow } from "../monitors/SettingsRow";
import { TrimSettingsDrawer } from "./TrimSettingsDrawer";

const TRIM_MODES: ModeOption[] = [
  { value: "off", label: "Off" },
  { value: "observe", label: "Observe" },
  { value: "lossless", label: "Lossless" },
  { value: "aggressive", label: "Aggressive" },
];

const TRIM_CONFIRM: Record<string, ModeConfirm> = {
  aggressive: {
    title: "Turn on Aggressive trimming?",
    description:
      "Also shortens very long tool descriptions at the start of the request. The first call after the change misses the provider's prompt cache once.",
    okText: "Aggressive",
  },
};

const LAST_7_DAYS: RangeSel = { range: "7d", from: "", to: "" };

/**
 * The Trimming row of the Savers card: the mode, what it did in the last 7 days (saved and likely saved apart, and in
 * observe the would-save, never summed) and the finer settings in a drawer.
 */
export function TrimSettings({
  workspaceId,
  readOnly,
}: {
  workspaceId: string;
  readOnly: boolean;
}) {
  const { token } = theme.useToken();
  const toast = useErrorToast();
  const config = useTrimConfig(workspaceId, true);
  const stats = useTrimStats(workspaceId, LAST_7_DAYS, true);
  const patch = usePatchTrim(workspaceId);
  const [open, setOpen] = useState(false);
  const cfg = config.data;
  if (!cfg) return null;
  const s = stats.data;
  const fail = { onError: (e: Error) => toast(e, "Could not update trimming.") };
  return (
    <>
      <SettingsRow
        title="Trimming"
        about="Shrinks bulky tool output (repeated lines, long logs, big JSON) before it is sent. Observe measures without changing anything."
      >
        <Flex vertical align="end" gap={token.marginXXS}>
          <ModeSegment
            label="Trimming mode"
            value={cfg.mode}
            options={TRIM_MODES}
            confirm={TRIM_CONFIRM}
            readOnly={readOnly}
            busy={patch.isPending}
            onChange={(mode) => patch.mutate({ mode }, fail)}
          />
          {s && parseMicro(s.wouldSaveUsd) > 0n && cfg.mode === "observe" ? (
            <Typography.Text type="secondary">
              {`Would save ${formatUsd(s.wouldSaveUsd)} (−${formatTokens(s.wouldTrimTokens)} tokens) in the last 7 days`}
            </Typography.Text>
          ) : null}
          {s && s.trimmedCalls > 0 ? (
            <Typography.Text type="secondary">
              {`Saved ${formatUsd(s.savedUsd)}, likely ${formatUsd(s.likelySavedUsd)} in the last 7 days`}
            </Typography.Text>
          ) : null}
          <Button size="small" type="link" onClick={() => setOpen(true)}>
            Settings
          </Button>
        </Flex>
      </SettingsRow>
      {open ? (
        <TrimSettingsDrawer
          config={cfg}
          tools={s?.byTool ?? []}
          readOnly={readOnly}
          saving={patch.isPending}
          onClose={() => setOpen(false)}
          onSave={(p) => patch.mutate(p, { ...fail, onSuccess: () => setOpen(false) })}
        />
      ) : null}
    </>
  );
}
