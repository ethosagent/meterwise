import { SearchOutlined } from "@ant-design/icons";
import { Avatar, Dropdown, Grid, Input, Typography, theme } from "antd";
import { useLocation, useNavigate } from "react-router-dom";
import { useMe } from "../api/identity";
import { useAuthContext } from "../auth/AuthProvider";
import { ThemeToggle } from "../theme/ThemeToggle";
import { initials } from "./initials";
import { pageByPath } from "./nav";

/** Top bar: page title, search, theme toggle, avatar menu. */
export function HeaderBar() {
  const { token } = theme.useToken();
  const screens = Grid.useBreakpoint();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const page = pageByPath(pathname);
  const user = useMe().data?.user;
  const { session } = useAuthContext();

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
            { key: "signout", label: "Sign out" },
          ],
          onClick: ({ key }) => {
            if (key === "profile") navigate("/settings/profile");
            if (key === "signout") void session.signOut();
          },
        }}
      >
        <button
          type="button"
          aria-label="Account menu"
          style={{ background: "none", border: 0, padding: 0, cursor: "pointer" }}
        >
          <Avatar style={{ background: token.colorPrimary }}>
            {initials(user?.name || user?.email || "")}
          </Avatar>
        </button>
      </Dropdown>
    </>
  );
}
