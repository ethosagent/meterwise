import { CopyOutlined } from "@ant-design/icons";
import { App, Button, theme } from "antd";

/** One ready-to-paste snippet, mono and selectable, with a Copy button. */
export function Snippet({ label, text }: { label: string; text: string }) {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      message.success(`${label} snippet copied`);
    } catch {
      message.error("Could not copy. Select the snippet and copy it by hand.");
    }
  };
  return (
    <div style={{ position: "relative" }}>
      <Button
        icon={<CopyOutlined />}
        size="small"
        aria-label={`Copy ${label} snippet`}
        onClick={copy}
        style={{ position: "absolute", top: token.marginXS, right: token.marginXS }}
      >
        Copy
      </Button>
      <figure aria-label={`${label} snippet`} style={{ margin: 0 }}>
        <pre
          style={{
            margin: 0,
            padding: token.paddingMD,
            paddingRight: token.controlHeightLG * 2,
            background: token.colorFillQuaternary,
            borderRadius: token.borderRadius,
            fontFamily: token.fontFamilyCode,
            fontSize: token.fontSizeSM,
            overflowX: "auto",
            whiteSpace: "pre-wrap",
            wordBreak: "break-all",
          }}
        >
          {text}
        </pre>
      </figure>
    </div>
  );
}
