import { Button, Flex, Input, Popconfirm, Select, Slider, Typography, theme } from "antd";
import { useState } from "react";
import { useForgetMemory, useMemoryConfig, useMemoryStats, usePatchMemory } from "../../api/memory";
import type { RangeSel } from "../../api/range";
import { useErrorToast } from "../../api/useErrorToast";
import { usePermission } from "../../auth/usePermission";
import type { MemoryConfig, MemoryConfigPatch, MemorySettings } from "../../contract/generated";
import { formatBp, formatUsd } from "../../lib/money";
import { type ModeConfirm, type ModeOption, ModeSegment } from "../monitors/ModeSegment";
import { SettingsRow } from "../monitors/SettingsRow";
import { DISABLED_NOTE } from "./memoryText";

const MEMORY_MODES: ModeOption[] = [
  { value: "off", label: "Off" },
  { value: "observe", label: "Observe" },
  { value: "hint", label: "Hint" },
];

const MEMORY_CONFIRM: Record<string, ModeConfirm> = {
  hint: {
    title: "Turn on Memory hints?",
    description:
      "A short hint from a past session is added to a later call of a similar task. The agent sees it.",
    okText: "Hint",
  },
};

const THIS_MONTH: RangeSel = { range: "30d", from: "", to: "" };

/** The model names, sent together and only the ones that changed. */
function Models({
  config,
  readOnly,
  busy,
  onSave,
}: {
  config: MemoryConfig;
  readOnly: boolean;
  busy: boolean;
  onSave: (p: MemoryConfigPatch) => void;
}) {
  const { token } = theme.useToken();
  const [model, setModel] = useState(config.model);
  const [embed, setEmbed] = useState(config.embedModel);
  const patch: MemoryConfigPatch = {
    ...(model !== config.model ? { model } : {}),
    ...(embed !== config.embedModel ? { embedModel: embed } : {}),
  };
  return (
    <Flex gap={token.marginXS} wrap align="center">
      <Input
        aria-label="Memory model"
        placeholder="Model (server default)"
        value={model}
        disabled={readOnly || busy}
        onChange={(e) => setModel(e.target.value)}
        style={{ width: token.controlHeightLG * 5 }}
      />
      <Input
        aria-label="Embedding model"
        placeholder="Embedding model (server default)"
        value={embed}
        disabled={readOnly || busy}
        onChange={(e) => setEmbed(e.target.value)}
        style={{ width: token.controlHeightLG * 5 }}
      />
      <Button
        size="small"
        disabled={readOnly || busy || Object.keys(patch).length === 0}
        onClick={() => onSave(patch)}
      >
        Save models
      </Button>
    </Flex>
  );
}

/** The finer settings under the mode switch: similarity threshold, hints per session and the two model names. */
function Fine({
  settings,
  readOnly,
  busy,
  onPatch,
}: {
  settings: MemorySettings;
  readOnly: boolean;
  busy: boolean;
  onPatch: (p: MemoryConfigPatch, onFail?: () => void) => void;
}) {
  const { token } = theme.useToken();
  const c = settings.config;
  const [bp, setBp] = useState(c.thresholdBp);
  return (
    <Flex vertical gap={token.marginSM} align="flex-end" aria-label="Memory settings">
      <Flex gap={token.marginXS} align="center" style={{ width: token.controlHeightLG * 10 }}>
        <Typography.Text>Similarity</Typography.Text>
        <Slider
          ariaLabelForHandle="Similarity threshold"
          style={{ flex: 1 }}
          min={5000}
          max={10000}
          step={100}
          value={bp}
          disabled={readOnly || busy}
          tooltip={{ formatter: (v) => formatBp(v ?? 0) }}
          onChange={setBp}
          onChangeComplete={(v) =>
            v !== c.thresholdBp && onPatch({ thresholdBp: v }, () => setBp(c.thresholdBp))
          }
        />
        <Typography.Text type="secondary">{formatBp(bp)}</Typography.Text>
      </Flex>
      <Flex gap={token.marginXS} align="center">
        <Typography.Text>Hints per session</Typography.Text>
        <Select
          aria-label="Max hints per session"
          value={c.maxHintsPerSession}
          disabled={readOnly || busy}
          options={[1, 2, 3].map((n) => ({ value: n, label: String(n) }))}
          onChange={(n) => onPatch({ maxHintsPerSession: n })}
        />
      </Flex>
      <Models
        key={`${c.model}|${c.embedModel}`}
        config={c}
        readOnly={readOnly}
        busy={busy}
        onSave={onPatch}
      />
    </Flex>
  );
}

/** "Forget memory": every trace of the workspace, behind a confirm; needs workspace:manage. */
function Forget({ workspaceId }: { workspaceId: string }) {
  const toast = useErrorToast();
  const forget = useForgetMemory(workspaceId);
  return (
    <Popconfirm
      title="Forget everything memory has learned?"
      description="Every remembered solution of this workspace is deleted. This cannot be undone."
      okText="Forget memory"
      okButtonProps={{ danger: true }}
      cancelText="Cancel"
      onConfirm={() =>
        forget.mutate(undefined, { onError: (e) => toast(e, "Could not forget memory.") })
      }
    >
      <Button danger size="small" loading={forget.isPending}>
        Forget memory
      </Button>
    </Popconfirm>
  );
}

/**
 * The Memory row of the Savers card: mode (hint waits behind a confirm), the finer settings, what memory's own model
 * calls cost this month, and Forget memory. Read-only without monitor_config:write; Forget needs workspace:manage.
 */
export function MemoryRow({
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
  const config = useMemoryConfig(workspaceId, true);
  const stats = useMemoryStats(workspaceId, THIS_MONTH, canStats);
  const patch = usePatchMemory(workspaceId);
  const canForget = usePermission("workspace:manage", workspaceId);
  const settings = config.data;
  if (!settings) return null;
  const reason = settings.disabledReason;
  const send = (p: MemoryConfigPatch, onFail?: () => void) =>
    patch.mutate(p, {
      onError: (e) => {
        onFail?.();
        toast(e, "Could not update memory.");
      },
    });
  return (
    <>
      <SettingsRow
        title="Memory"
        about="Remembers how past sessions solved a task and hints at it when a similar one starts. Savings from memory are always likely, never certain."
        note={settings.enabled || !reason ? null : (DISABLED_NOTE[reason] ?? reason)}
      >
        <Flex vertical align="flex-end" gap={token.marginXXS}>
          <ModeSegment
            label="Memory mode"
            value={settings.config.mode}
            options={MEMORY_MODES}
            confirm={MEMORY_CONFIRM}
            readOnly={readOnly}
            busy={patch.isPending}
            onChange={(mode) => send({ mode: mode as MemoryConfig["mode"] })}
          />
          {stats.data ? (
            <Typography.Text type="secondary">
              {`Memory's own model calls cost us ${formatUsd(stats.data.selfUsageUsdMonth)} this month.`}
            </Typography.Text>
          ) : null}
        </Flex>
      </SettingsRow>
      <Fine
        key={settings.config.thresholdBp}
        settings={settings}
        readOnly={readOnly}
        busy={patch.isPending}
        onPatch={send}
      />
      {canForget ? <Forget workspaceId={workspaceId} /> : null}
    </>
  );
}
