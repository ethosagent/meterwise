import { Card, Flex, Tag, Typography, theme } from "antd";
import type { CSSProperties } from "react";
import type { CauseView } from "../../contract/generated";
import { formatUsd, isPositiveMicro, maxMicro, sharePct } from "../../lib/money";
import { MODE_LABEL, isMode, monitorLabel } from "../monitors/monitorMeta";

type Kind = "certain" | "likely" | "would-save";

const KIND_NAME: Record<Kind, string> = {
  certain: "saved",
  likely: "likely saved",
  "would-save": "would save if enabled",
};

/** One bar, one of three styles: solid green (certain), solid grey (likely), amber outline (would save). */
function Bar({ kind, amount, scale }: { kind: Kind; amount: string; scale: string }) {
  const { token } = theme.useToken();
  const style: CSSProperties = {
    height: token.sizeXS,
    borderRadius: token.borderRadiusSM,
    width: `${Math.max(sharePct(amount, scale), 1)}%`,
    boxSizing: "border-box",
    ...(kind === "certain" && { background: token.colorSuccess }),
    ...(kind === "likely" && { background: token.colorTextQuaternary }),
    ...(kind === "would-save" && {
      background: "transparent",
      border: `${token.lineWidthBold}px ${token.lineType} ${token.colorWarning}`,
    }),
  };
  return (
    <Flex align="center" gap={token.marginXS}>
      <div style={{ flex: 1 }}>
        <div
          data-kind={kind}
          role="img"
          aria-label={`${KIND_NAME[kind]} ${formatUsd(amount)}`}
          style={style}
        />
      </div>
      <Typography.Text type="secondary" style={{ minWidth: token.sizeXXL * 2, textAlign: "end" }}>
        {formatUsd(amount)} {KIND_NAME[kind]}
      </Typography.Text>
    </Flex>
  );
}

/** Savings by cause: each monitor's certain, likely and would-save amounts as three separately styled bars, never merged. */
export function CauseBars({ causes }: { causes: CauseView[] }) {
  const { token } = theme.useToken();
  const scale = maxMicro(causes.flatMap((c) => [c.savedUsd, c.likelySavedUsd, c.wouldSaveUsd]));
  return (
    <Card title="Savings by cause" aria-label="Savings by cause">
      <Flex vertical gap={token.marginMD}>
        {causes.map((c) => {
          const kinds: [Kind, string][] = [
            ["certain", c.savedUsd],
            ["likely", c.likelySavedUsd],
            ["would-save", c.wouldSaveUsd],
          ];
          const shown = kinds.filter(([, v]) => isPositiveMicro(v));
          return (
            <div key={c.monitor} data-testid={`cause-${c.monitor}`}>
              <Flex gap={token.marginXS} align="center">
                <Typography.Text strong>{monitorLabel(c.monitor)}</Typography.Text>
                <Tag>{isMode(c.mode) ? MODE_LABEL[c.mode] : c.mode}</Tag>
                <Typography.Text type="secondary">
                  {c.fires} {c.fires === 1 ? "fire" : "fires"}
                </Typography.Text>
              </Flex>
              {shown.length === 0 ? (
                <Typography.Text type="secondary">Nothing yet</Typography.Text>
              ) : (
                shown.map(([kind, amount]) => (
                  <Bar key={kind} kind={kind} amount={amount} scale={scale} />
                ))
              )}
            </div>
          );
        })}
      </Flex>
    </Card>
  );
}
