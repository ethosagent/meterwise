import { Breadcrumb, Typography, theme } from "antd";
import { type NavPage, SETTINGS_LABEL } from "./nav";

/** Title, optional group breadcrumb and one-line description at the top of every page. */
export function PageHead({ page }: { page: NavPage }) {
  const { token } = theme.useToken();
  return (
    <div style={{ marginBottom: token.marginLG }}>
      {page.group ? (
        <Breadcrumb
          style={{ marginBottom: token.marginXS }}
          items={[{ title: SETTINGS_LABEL }, { title: page.label }]}
        />
      ) : null}
      <Typography.Title id="page-title" level={2} style={{ margin: 0 }}>
        {page.label}
      </Typography.Title>
      <Typography.Text type="secondary">{page.description}</Typography.Text>
    </div>
  );
}
