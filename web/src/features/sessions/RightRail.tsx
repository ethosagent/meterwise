import { CopyOutlined } from "@ant-design/icons";
import { Button, Card, Flex, Typography, theme } from "antd";
import { useCopy } from "../../api/useCopy";
import type { SessionDetail } from "../../contract/generated";
import { formatTokens } from "../../lib/money";

function Suggestion({ text }: { text: string }) {
  const copy = useCopy(text, "Suggestion");
  return (
    <Flex vertical gap="small">
      <Typography.Text>{text}</Typography.Text>
      <Button icon={<CopyOutlined />} onClick={() => void copy()} aria-label="Copy suggestion">
        Copy suggestion
      </Button>
    </Flex>
  );
}

/** "Why this session cost what it did": input tokens per call (context growth), repeats and a copyable suggestion. */
export function RightRail({ detail }: { detail: SessionDetail }) {
  const { token } = theme.useToken();
  const max = detail.calls.reduce((m, c) => Math.max(m, c.inputTokens), 0);
  const { repeats, suggestion } = detail.insight;
  return (
    <Flex vertical gap={token.marginMD}>
      <Card size="small" title="Why this session cost what it did">
        <Typography.Text type="secondary">Input tokens per call</Typography.Text>
        <Flex vertical gap={token.marginXXS} aria-label="Input tokens per call">
          {detail.calls.map((c) => (
            <Flex key={c.id} align="center" gap={token.marginXS}>
              <Typography.Text type="secondary">{`#${c.callIndex}`}</Typography.Text>
              <div style={{ flex: 1 }}>
                <div
                  data-testid={`ctx-${c.id}`}
                  style={{
                    height: token.sizeXS,
                    borderRadius: token.borderRadiusSM,
                    background: token.colorPrimary,
                    width: `${max > 0 ? Math.max(Math.round((c.inputTokens * 100) / max), 1) : 0}%`,
                  }}
                />
              </div>
              <Typography.Text>{formatTokens(c.inputTokens)}</Typography.Text>
            </Flex>
          ))}
        </Flex>
      </Card>
      <Card size="small" title="Repeats">
        {repeats.length === 0 ? (
          <Typography.Text type="secondary">No repeated reads or tool calls.</Typography.Text>
        ) : (
          <Flex vertical gap={token.marginXXS}>
            {repeats.map((r) => (
              <Typography.Text key={`${r.monitor}:${r.tool}:${r.path ?? r.pathHash ?? ""}`}>
                {r.text}
              </Typography.Text>
            ))}
          </Flex>
        )}
      </Card>
      {suggestion ? (
        <Card size="small" title="Prompt suggestion">
          <Suggestion text={suggestion} />
        </Card>
      ) : null}
    </Flex>
  );
}
