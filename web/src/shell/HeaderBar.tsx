import { SearchOutlined } from "@ant-design/icons";
import { Avatar, Dropdown, Grid, Input, Typography, theme } from "antd";
import { useLocation, useNavigate } from "react-router-dom";
import { ThemeToggle } from "../theme/ThemeToggle";
import { pageByPath } from "./nav";
import { initials, STUB_USER } from "./stub";

/** Top bar: page title, search, theme toggle, avatar menu. */
export function HeaderBar() {
  const { token } = theme.useToken();
  const screens = Grid.useBreakpoint();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const page = pageByPath(pathname);

  return (
    <>
      <Typography.Text strong ellipsis style={{ flex: 1, fontSize: token.fontSizeLG }}>
        {page?.label ?? ""}
      </Typography.Text>
      {screens.md ? (
        <Input
          allowClear
          aria-label="Search"
          placeholder="Search"
          prefix={<SearchOutlined />}
          style={{ width: token.controlHeightLG * 6 }}
        />
      ) : null}
      <ThemeToggle />
      <Dropdown
        trigger={["click"]}
        menu={{
          items: [
            { key: "profile", label: "Profile" },
            { key: "signout", label: "Sign out", disabled: true },
          ],
          onClick: ({ key }) => key === "profile" && navigate("/settings/profile"),
        }}
      >
        <button
          type="button"
          aria-label="Account menu"
          style={{ background: "none", border: 0, padding: 0, cursor: "pointer" }}
        >
          <Avatar style={{ background: token.colorPrimary }}>{initials(STUB_USER.name)}</Avatar>
        </button>
      </Dropdown>
    </>
  );
}
