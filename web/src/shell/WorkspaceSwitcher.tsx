import { AppstoreOutlined, DownOutlined } from "@ant-design/icons";
import { App, Menu, Popover, Tooltip, Typography, theme } from "antd";
import { useState } from "react";
import { useActiveWorkspace, useMe, useSwitchWorkspace, useWorkspaces } from "../api/identity";
import { hasPermission } from "../auth/permissions";
import { useUi } from "../state/ui";
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
  const openCreate = useUi((s) => s.setCreateWorkspaceOpen);
  const [open, setOpen] = useState(false);
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
      {/* A Popover, not a Dropdown: Dropdown forces every Menu inside it to vertical, and a vertical Menu
          in the Sider reduces its items to initials (the Sider's collapsed context). A Popover lets an inline
          Menu opt out with inlineCollapsed=false. antd has no public way to reset the Sider context. */}
      <Popover
        trigger="click"
        open={open}
        onOpenChange={setOpen}
        placement="bottomLeft"
        arrow={false}
        styles={{
          root: { minWidth: SIDER_WIDTH - 2 * token.paddingSM },
          container: { padding: token.paddingXXS },
        }}
        content={
          <Menu
            mode="inline"
            inlineCollapsed={false}
            selectable
            selectedKeys={active ? [active.id] : []}
            items={[
              ...workspaces.map((w) => ({ key: w.id, label: w.name })),
              ...(canCreate
                ? [{ type: "divider" as const }, { key: CREATE_KEY, label: "＋ New workspace" }]
                : []),
            ]}
            onClick={({ key }) => {
              setOpen(false);
              if (key === CREATE_KEY) return openCreate(true);
              if (key === active?.id) return;
              switchTo.mutate(key, {
                onError: () => message.error("Could not switch workspace."),
              });
            }}
            style={{ background: "transparent", borderInlineEnd: 0 }}
          />
        }
      >
        {collapsed ? (
          <Tooltip title={name} placement="right">
            {trigger}
          </Tooltip>
        ) : (
          trigger
        )}
      </Popover>
    </div>
  );
}
