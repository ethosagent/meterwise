import { CopyOutlined } from "@ant-design/icons";
import { Button, Input, Space } from "antd";
import { useCopy } from "../../api/useCopy";

/** Read-only proxy URL with a copy button. The URL is the whole workspace setup. */
export function ProxyUrl({ url }: { url: string }) {
  const copy = useCopy(url, "Proxy URL");
  return (
    <Space.Compact style={{ width: "100%" }}>
      <Input readOnly aria-label="Proxy URL" value={url} onFocus={(e) => e.target.select()} />
      <Button icon={<CopyOutlined />} onClick={copy}>
        Copy
      </Button>
    </Space.Compact>
  );
}
