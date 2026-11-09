import { CopyOutlined } from "@ant-design/icons";
import { App, Button, Input, Space } from "antd";

/** Read-only proxy URL with a copy button. The URL is the whole workspace setup. */
export function ProxyUrl({ url }: { url: string }) {
  const { message } = App.useApp();
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      message.success("Proxy URL copied");
    } catch {
      message.error("Could not copy. Select the URL and copy it by hand.");
    }
  };
  return (
    <Space.Compact style={{ width: "100%" }}>
      <Input readOnly aria-label="Proxy URL" value={url} onFocus={(e) => e.target.select()} />
      <Button icon={<CopyOutlined />} onClick={copy}>
        Copy
      </Button>
    </Space.Compact>
  );
}
