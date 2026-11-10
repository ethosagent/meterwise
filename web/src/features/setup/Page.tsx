import { Alert, Card, Skeleton, Tabs, Typography, theme } from "antd";
import { useActiveWorkspace } from "../../api/identity";
import { useSetup } from "../../api/setup";
import { usePermission } from "../../auth/usePermission";
import type { Setup } from "../../contract/generated";
import { pageByKey } from "../../shell/nav";
import { PageHead } from "../../shell/PageHead";
import { ActiveWorkspaceGate } from "../../shell/WorkspaceGuard";
import { UrlHidden, WorkspaceUrl } from "../workspace/WorkspaceUrl";
import { Snippet } from "./Snippet";
import { Status } from "./Status";

const TABS: { key: keyof Setup["snippets"]; label: string }[] = [
  { key: "openaiEnv", label: "OpenAI SDK" },
  { key: "anthropicEnv", label: "Anthropic SDK" },
  { key: "opencodeJson", label: "OpenCode" },
  { key: "claudeCodeEnv", label: "Claude Code" },
  { key: "curl", label: "curl" },
];

/** Setup: the workspace URL is the whole onboarding. Without workspace_url:read the page says the URL is hidden. */
export function SetupPage() {
  const { token } = theme.useToken();
  const active = useActiveWorkspace();
  const ws = active.state === "ready" ? active.workspace : null;
  const canRead = usePermission("workspace_url:read", ws?.id ?? null);
  const visible = ws !== null && canRead && ws.proxyUrl !== null;
  const setup = useSetup(ws?.id ?? null, visible);
  const gap = { marginTop: token.marginLG };
  return (
    <section aria-labelledby="page-title">
      <PageHead page={pageByKey("setup")} />
      <ActiveWorkspaceGate active={active}>
        {(w) =>
          !visible ? (
            <UrlHidden />
          ) : (
            <>
              <Card title="Your proxy URL">
                <WorkspaceUrl ws={w} />
              </Card>
              <Card title="Point your agent at it" style={gap}>
                <Typography.Paragraph>
                  Keep using your own provider key. The proxy never asks for it and never stores it.
                </Typography.Paragraph>
                {setup.data ? (
                  <Tabs
                    items={TABS.map((t) => ({
                      key: t.key,
                      label: t.label,
                      children: <Snippet label={t.label} text={setup.data.snippets[t.key]} />,
                    }))}
                  />
                ) : setup.isError ? (
                  <Alert type="error" showIcon title="Could not load the setup snippets." />
                ) : (
                  <Skeleton active />
                )}
              </Card>
              <Card title="Status" style={gap}>
                {setup.data ? <Status status={setup.data.status} /> : <Skeleton.Input active />}
              </Card>
            </>
          )
        }
      </ActiveWorkspaceGate>
    </section>
  );
}
