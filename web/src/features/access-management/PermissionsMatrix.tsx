import { Checkbox, Table, Typography, theme } from "antd";
import {
  type Action,
  type Grant,
  RESOURCE_TYPES,
  type ResourceType,
  TAXONOMY,
} from "../../access/plan03-types";

const ACTIONS: Action[] = ["read", "write", "manage"];

export interface MatrixSection {
  /** Heading: the workspace's name, or "Organization". */
  title: string;
  /** The workspace the grants in this section apply to; null for the org-scoped section. */
  workspaceId: string | null;
}

function same(g: Grant, workspaceId: string | null, r: ResourceType, a: Action) {
  return g.workspaceId === workspaceId && g.resourceType === r && g.action === a;
}

interface Props {
  sections: MatrixSection[];
  grants: Grant[];
  onChange: (next: Grant[]) => void;
  disabled: boolean;
}

/** One table per attached workspace (resourceType x action) plus one for the org-scoped resources. */
export function PermissionsMatrix({ sections, grants, onChange, disabled }: Props) {
  const { token } = theme.useToken();
  const toggle = (workspaceId: string | null, r: ResourceType, a: Action, on: boolean) =>
    onChange(
      on
        ? [...grants, { workspaceId, resourceType: r, action: a }]
        : grants.filter((g) => !same(g, workspaceId, r, a)),
    );
  return (
    <>
      {sections.map((s) => {
        const scope = s.workspaceId === null ? "org" : "workspace";
        const rows = RESOURCE_TYPES.filter((r) => TAXONOMY[r].scope === scope);
        return (
          <div key={s.workspaceId ?? "org"} style={{ marginBottom: token.marginLG }}>
            <Typography.Title level={5}>{s.title}</Typography.Title>
            <Table<ResourceType>
              size="small"
              pagination={false}
              rowKey={(r) => r}
              dataSource={rows}
              scroll={{ x: "max-content" }}
              aria-label={`Permissions for ${s.title}`}
              columns={[
                { title: "Resource", key: "r", render: (_: unknown, r) => <code>{r}</code> },
                ...ACTIONS.map((a) => ({
                  title: a,
                  key: a,
                  align: "center" as const,
                  render: (_: unknown, r: ResourceType) =>
                    (TAXONOMY[r].actions as readonly string[]).includes(a) ? (
                      <Checkbox
                        aria-label={`${s.title}: ${r}:${a}`}
                        disabled={disabled}
                        checked={grants.some((g) => same(g, s.workspaceId, r, a))}
                        onChange={(e) => toggle(s.workspaceId, r, a, e.target.checked)}
                      />
                    ) : (
                      <Typography.Text type="secondary">-</Typography.Text>
                    ),
                })),
              ]}
            />
          </div>
        );
      })}
    </>
  );
}
