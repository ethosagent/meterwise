import { ConfigProvider, Layout, Typography, theme } from "antd";
import { useEffect } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { siderTheme } from "../theme/theme";
import { useAppName } from "./Brand";
import { HeaderBar } from "./HeaderBar";
import { pageByPath } from "./nav";
import { SiderPanel } from "./SiderPanel";

/** Keeps the browser tab title in step with the page and the configured brand. */
function useDocumentTitle() {
  const { pathname } = useLocation();
  const appName = useAppName();
  const label = pageByPath(pathname)?.label;
  useEffect(() => {
    if (appName) document.title = label ? `${label} · ${appName}` : appName;
  }, [label, appName]);
}

/** The application frame: dark sider, header, content, footer. */
export function Shell() {
  const { token } = theme.useToken();
  const appName = useAppName();
  useDocumentTitle();

  return (
    <Layout hasSider style={{ minHeight: "100vh" }}>
      <ConfigProvider theme={siderTheme}>
        <SiderPanel />
      </ConfigProvider>
      <Layout style={{ minWidth: 0 }}>
        <Layout.Header
          style={{
            display: "flex",
            alignItems: "center",
            gap: token.marginMD,
            borderBottom: `${token.lineWidth}px ${token.lineType} ${token.colorBorderSecondary}`,
            position: "sticky",
            top: 0,
            zIndex: 1,
          }}
        >
          <HeaderBar />
        </Layout.Header>
        <Layout.Content style={{ padding: token.paddingLG, flex: 1 }}>
          <Outlet />
        </Layout.Content>
        <Layout.Footer style={{ textAlign: "center" }}>
          <Typography.Text type="secondary">
            © {new Date().getFullYear()} {appName ?? ""}
          </Typography.Text>
        </Layout.Footer>
      </Layout>
    </Layout>
  );
}
