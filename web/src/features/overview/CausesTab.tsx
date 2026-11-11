import { CopyOutlined } from "@ant-design/icons";
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Flex,
  Row,
  Skeleton,
  Table,
  Tag,
  Typography,
  theme,
} from "antd";
import { Link } from "react-router-dom";
import type { RangeSel } from "../../api/range";
import { useSavingsCauses } from "../../api/savings";
import { useCopy } from "../../api/useCopy";
import { useToastOnError } from "../../api/useToastOnError";
import type { CauseCard, ClientRow, Suggestion } from "../../contract/generated";
import { formatBp, formatTokens, formatUsd, isPositiveMicro } from "../../lib/money";
import { causeLabel, modeLabel } from "./causeMeta";
import { ConfidenceTag } from "./ConfidenceTag";

/** The most repeated target of a cause: the path when one was stored, else the tool and the 8-hex hash prefix. */
function topTarget(c: CauseCard): string | null {
  if (c.topPath) return c.topPath;
  if (c.topTool && c.topPathHash) return `${c.topTool} #${c.topPathHash.slice(0, 8)}`;
  return c.topTool;
}

function CauseCardView({ cause }: { cause: CauseCard }) {
  const target = topTarget(cause);
  return (
    <Card
      size="small"
      title={causeLabel(cause.cause)}
      extra={<Tag>{modeLabel(cause.mode)}</Tag>}
      data-testid={`cause-card-${cause.cause}`}
    >
      <Flex vertical gap="small">
        <div>
          <Typography.Text type="secondary">Saved </Typography.Text>
          <Typography.Text strong>{formatUsd(cause.savedUsd)}</Typography.Text>{" "}
          <Typography.Text type="secondary">
            ({formatTokens(cause.savedTokens)} tokens)
          </Typography.Text>
        </div>
        <div>
          <Typography.Text type="secondary">Likely saved </Typography.Text>
          <Typography.Text strong>{formatUsd(cause.likelySavedUsd)}</Typography.Text>
        </div>
        {isPositiveMicro(cause.wouldSaveUsd) ? (
          <div>
            <Typography.Text type="secondary">Would save if enabled </Typography.Text>
            <Typography.Text strong>{formatUsd(cause.wouldSaveUsd)}</Typography.Text>
          </div>
        ) : null}
        <Flex gap="small" align="center" wrap>
          <Typography.Text type="secondary">
            {cause.fires} {cause.fires === 1 ? "fire" : "fires"}
          </Typography.Text>
          <ConfidenceTag confidence={cause.confidence} />
        </Flex>
        {cause.topClient || target ? (
          <Typography.Text type="secondary">
            {cause.topClient ? `Top client ${cause.topClient}. ` : ""}
            {target
              ? `Most repeated ${target}${cause.topRepeatCount > 0 ? ` (${cause.topRepeatCount}x)` : ""}.`
              : ""}
          </Typography.Text>
        ) : null}
      </Flex>
    </Card>
  );
}

function avgDelta(c: ClientRow): string {
  if (c.prevAvgCallsPerSession === null) return "no earlier window";
  const d = c.avgCallsPerSession - c.prevAvgCallsPerSession;
  if (d === 0) return "same as the window before";
  return `${d > 0 ? "+" : "-"}${Math.abs(d).toFixed(1)} vs the window before`;
}

function ClientsTable({ rows }: { rows: ClientRow[] }) {
  return (
    <Card title="By client" aria-label="By client">
      <Table<ClientRow>
        rowKey="client"
        size="small"
        pagination={false}
        dataSource={rows}
        locale={{ emptyText: <Empty description="No client activity in this range" /> }}
        columns={[
          { title: "Client", dataIndex: "client" },
          { title: "Spent", render: (_, r) => formatUsd(r.spentUsd) },
          { title: "Saved", render: (_, r) => formatUsd(r.savedUsd) },
          { title: "Likely saved", render: (_, r) => formatUsd(r.likelySavedUsd) },
          { title: "Saved %", render: (_, r) => formatBp(r.savedBp) },
          { title: "Blocked", dataIndex: "blocked" },
          { title: "Calls", dataIndex: "calls" },
          { title: "Sessions", dataIndex: "sessions" },
          {
            title: "Avg calls per session",
            render: (_, r) => (
              <>
                {r.avgCallsPerSession.toFixed(1)}{" "}
                <Typography.Text type="secondary">({avgDelta(r)})</Typography.Text>
              </>
            ),
          },
        ]}
      />
    </Card>
  );
}

function SuggestionRow({ s }: { s: Suggestion }) {
  const copy = useCopy(s.text, "Suggestion");
  return (
    <Flex justify="space-between" align="center" gap="middle">
      <Typography.Text>{s.text}</Typography.Text>
      <Button icon={<CopyOutlined />} onClick={() => void copy()} aria-label="Copy suggestion">
        Copy
      </Button>
    </Flex>
  );
}

/** Where it came from: cause cards, the per-client table, and fixed-template prompt suggestions. */
export function CausesTab({ workspaceId, sel }: { workspaceId: string; sel: RangeSel }) {
  const { token } = theme.useToken();
  const q = useSavingsCauses(workspaceId, sel, true);
  useToastOnError(q.error, "Could not load where your savings came from.");
  if (q.isError)
    return <Alert type="error" showIcon title="Could not load where your savings came from." />;
  if (!q.data) return <Skeleton active />;
  const { causes, clients, suggestions } = q.data;
  const quiet = clients.length === 0 && causes.every((c) => c.fires === 0);
  if (quiet) {
    return (
      <Card>
        <Empty description="Nothing has fired in this range">
          <Link to="/setup">
            <Button>Go to Setup</Button>
          </Link>
        </Empty>
      </Card>
    );
  }
  return (
    <Flex vertical gap={token.marginLG}>
      <Row gutter={[token.marginMD, token.marginMD]}>
        {causes.map((c) => (
          <Col key={c.cause} xs={24} md={12} xl={8}>
            <CauseCardView cause={c} />
          </Col>
        ))}
      </Row>
      <ClientsTable rows={clients} />
      <Card title="Prompt suggestions" aria-label="Prompt suggestions">
        {suggestions.length === 0 ? (
          <Empty description="No single file or tool dominates your repeated reads" />
        ) : (
          <Flex vertical gap="small">
            {suggestions.map((s) => (
              <SuggestionRow key={`${s.monitor}:${s.tool}:${s.path ?? s.pathHash ?? ""}`} s={s} />
            ))}
          </Flex>
        )}
      </Card>
    </Flex>
  );
}
