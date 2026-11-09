import { Layout, Menu, theme } from "antd";
import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { usePermissionSnapshot } from "../api/access";
import { hasPermission } from "../auth/permissions";
import { useUi } from "../state/ui";
import { SIDER_COLLAPSED_WIDTH, SIDER_WIDTH } from "../theme/theme";
import { Brand } from "./Brand";
import {
  MAIN_PAGES,
  type NavPage,
  pageByPath,
  SETTINGS_ICON,
  SETTINGS_LABEL,
  SETTINGS_PAGES,
} from "./nav";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";

const SETTINGS_KEY = "settings";

/** Destinations the caller may open: a page that names a permission is hidden without it. */
function menuItems(allowed: (p: NavPage) => boolean) {
  const item = (p: NavPage) => ({ key: p.path, icon: p.icon, label: p.label });
  return [
    ...MAIN_PAGES.filter(allowed).map(item),
    {
      key: SETTINGS_KEY,
      icon: SETTINGS_ICON,
      label: SETTINGS_LABEL,
      children: SETTINGS_PAGES.filter(allowed).map(item),
    },
  ];
}

/** Collapsible sidebar: brand, workspace switcher, navigation. Its tokens come from the nested dark provider. */
export function SiderPanel() {
  const { token } = theme.useToken();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const collapsed = useUi((s) => s.siderCollapsed);
  const setCollapsed = useUi((s) => s.setSiderCollapsed);
  const snapshot = usePermissionSnapshot().data;
  const items = menuItems((p) => !p.permission || hasPermission(snapshot, p.permission));
  const page = pageByPath(pathname);
  const inSettings = page?.group !== undefined;
  const [openKeys, setOpenKeys] = useState<string[]>(inSettings ? [SETTINGS_KEY] : []);
  // The shell outlives route changes, so navigating onto a settings page (menu, avatar, link)
  // opens its group; the user can still close it by hand afterwards.
  useEffect(() => {
    if (inSettings)
      setOpenKeys((keys) => (keys.includes(SETTINGS_KEY) ? keys : [...keys, SETTINGS_KEY]));
  }, [inSettings]);

  return (
    <Layout.Sider
      collapsible
      theme="dark"
      breakpoint="md"
      collapsed={collapsed}
      onCollapse={setCollapsed}
      width={SIDER_WIDTH}
      collapsedWidth={SIDER_COLLAPSED_WIDTH}
      style={{ position: "sticky", top: 0, height: "100vh", overflow: "auto" }}
    >
      <Brand collapsed={collapsed} />
      <WorkspaceSwitcher collapsed={collapsed} />
      <Menu
        theme="dark"
        mode="inline"
        aria-label="Main navigation"
        selectedKeys={page ? [page.path] : []}
        openKeys={openKeys}
        onOpenChange={setOpenKeys}
        items={items}
        onClick={({ key }) => navigate(key)}
        style={{ borderInlineEnd: 0, paddingInline: token.paddingXXS }}
      />
    </Layout.Sider>
  );
}
