import { AppstoreOutlined, DownOutlined } from "@ant-design/icons";
import { App, Dropdown, Tooltip, Typography, theme } from "antd";
import { useState } from "react";
import { useActiveWorkspace, useMe, useSwitchWorkspace, useWorkspaces } from "../api/identity";
import { hasPermission } from "../auth/permissions";
import { CreateWorkspaceDialog } from "../features/workspace/CreateWorkspaceDialog";
import { SIDER_WIDTH } from "../theme/theme";

const CREATE_KEY = "__create";

/** Workspace row only: one org, many workspaces, so there is no org switcher. */
export function WorkspaceSwitcher({ collapsed }: { collapsed: boolean }) {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const me = useMe().data;
  const workspaces = useWorkspaces().data ?? [];
  const active = useActiveWorkspace();
  const switchTo = useSwitchWorkspace();
  const [creating, setCreating] = useState(false);
  const name = active?.name ?? "";
  const canCreate = hasPermission(me, "workspaces.manage");

  const trigger = (
    <button
      type="button"
      aria-label={`Workspace: ${name}`}
      data-testid="workspace-switcher"
      style={{
        display: "flex",
        alignItems: "center",
        gap: token.marginXS,
        width: "100%",
        padding: token.paddingXS,
        justifyContent: collapsed ? "center" : "flex-start",
        background: token.colorBgElevated,
        border: `${token.lineWidth}px ${token.lineType} ${token.colorBorder}`,
        borderRadius: token.borderRadius,
        color: token.colorText,
        cursor: "pointer",
        textAlign: "left",
      }}
    >
      <AppstoreOutlined style={{ color: token.colorPrimary }} />
      {collapsed ? null : (
        <>
          <span style={{ flex: 1, minWidth: 0 }}>
            <Typography.Text
              type="secondary"
              style={{ display: "block", fontSize: token.fontSizeSM }}
            >
              Workspace
            </Typography.Text>
            <Typography.Text ellipsis style={{ display: "block" }}>
              {name}
            </Typography.Text>
          </span>
          <DownOutlined style={{ fontSize: token.fontSizeSM, color: token.colorTextSecondary }} />
        </>
      )}
    </button>
  );

  return (
    <div style={{ padding: `0 ${token.paddingSM}px ${token.paddingSM}px` }}>
      <CreateWorkspaceDialog open={creating} onClose={() => setCreating(false)} />
      <Dropdown
        trigger={["click"]}
        // The popup is as wide as its trigger; on the collapsed rail that is a few pixels and clips the names.
        styles={{ root: { minWidth: SIDER_WIDTH - 2 * token.paddingSM } }}
        menu={{
          selectable: true,
          // The Sider tells every menu below it that it is collapsed, which would reduce these items to initials.
          inlineCollapsed: false,
          selectedKeys: active ? [active.id] : [],
          items: [
            ...workspaces.map((w) => ({ key: w.id, label: w.name })),
            ...(canCreate
              ? [{ type: "divider" as const }, { key: CREATE_KEY, label: "＋ New workspace" }]
              : []),
          ],
          onClick: ({ key }) => {
            if (key === CREATE_KEY) return setCreating(true);
            if (key === active?.id) return;
            switchTo.mutate(key, { onError: () => message.error("Could not switch workspace.") });
          },
        }}
      >
        {collapsed ? (
          <Tooltip title={name} placement="right">
            {trigger}
          </Tooltip>
        ) : (
          trigger
        )}
      </Dropdown>
    </div>
  );
}
