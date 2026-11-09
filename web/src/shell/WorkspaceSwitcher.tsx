import { AppstoreOutlined, DownOutlined } from "@ant-design/icons";
import { Dropdown, Tooltip, Typography, theme } from "antd";
import { useUi } from "../state/ui";
import { STUB_WORKSPACES } from "./stub";

/** Workspace row only: one org, many workspaces, so there is no org switcher. */
export function WorkspaceSwitcher({ collapsed }: { collapsed: boolean }) {
  const { token } = theme.useToken();
  const activeId = useUi((s) => s.activeWorkspaceId);
  const setActive = useUi((s) => s.setActiveWorkspaceId);
  const active = STUB_WORKSPACES.find((w) => w.id === activeId) ?? STUB_WORKSPACES[0];
  const name = active?.name ?? "";

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
      <Dropdown
        trigger={["click"]}
        menu={{
          selectable: true,
          selectedKeys: [activeId],
          items: STUB_WORKSPACES.map((w) => ({ key: w.id, label: w.name })),
          onClick: ({ key }) => setActive(key),
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
