import { Skeleton, Typography, theme } from "antd";
import { useConfig } from "../api/config";

/** Shown only when /api/config fails, so the sidebar never goes blank. */
export const FALLBACK_BRAND = "Dashboard";

/** The brand name for the deployment, from /api/config; nothing is hardcoded. */
export function useAppName(): string | undefined {
  const q = useConfig();
  if (q.isError) return FALLBACK_BRAND;
  return q.data?.appName;
}

export function Brand({ collapsed }: { collapsed: boolean }) {
  const { token } = theme.useToken();
  const name = useAppName();
  const mark = (name ?? "").charAt(0).toUpperCase();
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: token.marginXS,
        padding: token.paddingMD,
        justifyContent: collapsed ? "center" : "flex-start",
      }}
    >
      <span
        aria-hidden="true"
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: token.controlHeight,
          height: token.controlHeight,
          flex: "none",
          borderRadius: token.borderRadius,
          background: token.colorPrimary,
          color: token.colorBgLayout,
          fontWeight: token.fontWeightStrong,
        }}
      >
        {mark}
      </span>
      {collapsed ? null : name === undefined ? (
        <Skeleton.Input active size="small" />
      ) : (
        <Typography.Text
          strong
          data-testid="brand-name"
          style={{ fontSize: token.fontSizeLG, color: token.colorTextHeading }}
        >
          {name}
        </Typography.Text>
      )}
    </div>
  );
}
