import { Flex, Typography, theme } from "antd";
import type { ReactNode } from "react";

/** One settings line: what it is and what it does on the left, its control on the right. */
export function SettingsRow({
  title,
  about,
  note,
  children,
}: {
  title: string;
  about: string;
  /** A line under the description, such as why a feature is not enabled. */
  note?: string | null;
  children: ReactNode;
}) {
  const { token } = theme.useToken();
  return (
    <Flex justify="space-between" align="center" gap={token.marginMD} wrap>
      <Flex vertical gap={token.marginXXS} style={{ flex: 1, minWidth: token.controlHeightLG * 6 }}>
        <Typography.Text strong>{title}</Typography.Text>
        <Typography.Text type="secondary">{about}</Typography.Text>
        {note ? <Typography.Text type="warning">{note}</Typography.Text> : null}
      </Flex>
      {children}
    </Flex>
  );
}
