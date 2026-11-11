import { Button, Drawer, Flex, InputNumber, Select, Switch, Table, Typography, theme } from "antd";
import { useState } from "react";
import type { TrimConfig, TrimConfigPatch, TrimToolStat } from "../../contract/generated";
import { formatTokens } from "../../lib/money";
import { STAGES } from "./trimMeta";

/** The most tools the server accepts in the never-trim list (contract.MaxNeverTrimTools). */
const MAX_NEVER = 50;

/** Only the fields that differ from `base`, as a partial PATCH body. Stages merge per stage on the server. */
export function changedFields(base: TrimConfig, next: TrimConfig): TrimConfigPatch {
  const patch: TrimConfigPatch = {};
  if (next.maxFoldPct !== base.maxFoldPct) patch.maxFoldPct = next.maxFoldPct;
  if (next.minGainPct !== base.minGainPct) patch.minGainPct = next.minGainPct;
  if (next.neverTrimTools.join("\n") !== base.neverTrimTools.join("\n")) {
    patch.neverTrimTools = next.neverTrimTools;
  }
  const stages = STAGES.filter((s) => next.stages[s.stage] !== base.stages[s.stage]);
  if (stages.length > 0) {
    patch.stages = Object.fromEntries(stages.map((s) => [s.stage, next.stages[s.stage] === true]));
  }
  return patch;
}

/**
 * The finer trim settings: which stages run, tools that are never trimmed, how much of a result may be folded and the
 * least gain worth a rewrite, plus the by-tool table. Save sends only what changed.
 */
export function TrimSettingsDrawer({
  config,
  tools,
  readOnly,
  saving,
  onClose,
  onSave,
}: {
  config: TrimConfig;
  tools: readonly TrimToolStat[];
  readOnly: boolean;
  saving: boolean;
  onClose: () => void;
  onSave: (patch: TrimConfigPatch) => void;
}) {
  const { token } = theme.useToken();
  const [draft, setDraft] = useState<TrimConfig>(config);
  const patch = changedFields(config, draft);
  const dirty = Object.keys(patch).length > 0;
  const setStage = (stage: string, on: boolean) =>
    setDraft({ ...draft, stages: { ...draft.stages, [stage]: on } });
  return (
    <Drawer
      open
      onClose={onClose}
      title="Trimming settings"
      size="large"
      footer={
        <Flex justify="end" gap={token.marginXS}>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="primary"
            disabled={readOnly || !dirty}
            loading={saving}
            onClick={() => onSave(patch)}
          >
            Save
          </Button>
        </Flex>
      }
    >
      <Flex vertical gap={token.marginMD}>
        <Typography.Text strong>Stages</Typography.Text>
        {STAGES.map((s) => (
          <Flex key={s.stage} justify="space-between" align="center" gap={token.marginSM}>
            <Flex vertical>
              <Typography.Text id={`stage-${s.stage}`}>{s.label}</Typography.Text>
              <Typography.Text type="secondary">{s.about}</Typography.Text>
            </Flex>
            <Switch
              aria-labelledby={`stage-${s.stage}`}
              disabled={readOnly}
              checked={draft.stages[s.stage] === true}
              onChange={(on) => setStage(s.stage, on)}
            />
          </Flex>
        ))}
        <Typography.Text strong id="never-trim">
          Never trim these tools
        </Typography.Text>
        <Select
          mode="tags"
          aria-labelledby="never-trim"
          disabled={readOnly}
          value={draft.neverTrimTools}
          tokenSeparators={[","]}
          maxCount={MAX_NEVER}
          placeholder="Tool names, for example read_file"
          onChange={(v: string[]) =>
            setDraft({ ...draft, neverTrimTools: v.map((t) => t.trim()).filter(Boolean) })
          }
        />
        <Flex justify="space-between" align="center" gap={token.marginSM}>
          <Flex vertical>
            <Typography.Text id="max-fold">Most of a result that may be folded (%)</Typography.Text>
            <Typography.Text type="secondary">
              A log is not folded past this share of its size.
            </Typography.Text>
          </Flex>
          <InputNumber
            aria-labelledby="max-fold"
            min={10}
            max={100}
            precision={0}
            disabled={readOnly}
            value={draft.maxFoldPct}
            onChange={(v) => typeof v === "number" && setDraft({ ...draft, maxFoldPct: v })}
          />
        </Flex>
        <Flex justify="space-between" align="center" gap={token.marginSM}>
          <Flex vertical>
            <Typography.Text id="min-gain">Least saving worth a rewrite (%)</Typography.Text>
            <Typography.Text type="secondary">
              A stage that saves less than this share of a result is left out.
            </Typography.Text>
          </Flex>
          <InputNumber
            aria-labelledby="min-gain"
            min={0}
            max={50}
            precision={0}
            disabled={readOnly}
            value={draft.minGainPct}
            onChange={(v) => typeof v === "number" && setDraft({ ...draft, minGainPct: v })}
          />
        </Flex>
        <Typography.Text strong>By tool, last 7 days</Typography.Text>
        <Table<TrimToolStat>
          size="small"
          pagination={false}
          rowKey="tool"
          dataSource={[...tools]}
          locale={{ emptyText: "No trimmed calls yet." }}
          columns={[
            { title: "Tool", dataIndex: "tool", key: "tool" },
            { title: "Times trimmed", dataIndex: "fires", key: "fires", align: "right" },
            {
              title: "Tokens saved",
              key: "tokens",
              align: "right",
              render: (_, r) => formatTokens(r.tokens),
            },
          ]}
        />
      </Flex>
    </Drawer>
  );
}
