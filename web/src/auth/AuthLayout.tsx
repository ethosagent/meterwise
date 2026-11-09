import { Card, Flex, Typography, theme } from "antd";
import type { ReactNode } from "react";
import { useAppName } from "../shell/Brand";

/** Centered frame for every signed-out screen. */
export function AuthLayout({ children }: { children: ReactNode }) {
  const { token } = theme.useToken();
  const appName = useAppName();
  return (
    <Flex
      vertical
      align="center"
      justify="center"
      gap={token.marginLG}
      style={{ minHeight: "100vh", padding: token.paddingMD }}
    >
      <Typography.Title level={3} style={{ margin: 0 }}>
        {appName ?? ""}
      </Typography.Title>
      {children}
    </Flex>
  );
}

/** The card holding a signed-out form, sized from tokens. */
export function AuthCard({ children }: { children: ReactNode }) {
  const { token } = theme.useToken();
  return <Card style={{ width: "100%", maxWidth: token.controlHeightLG * 10 }}>{children}</Card>;
}
