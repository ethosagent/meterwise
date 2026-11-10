import { Tag, theme } from "antd";

/** certain = green, likely = grey, anything else (none) = default; the same colours as the bars. */
export function ConfidenceTag({ confidence }: { confidence: string }) {
  const { token } = theme.useToken();
  if (confidence === "certain") return <Tag color="success">certain</Tag>;
  if (confidence === "likely") {
    return <Tag style={{ color: token.colorTextSecondary }}>likely</Tag>;
  }
  return <Tag>{confidence}</Tag>;
}
