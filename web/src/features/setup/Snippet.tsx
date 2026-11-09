import { CopyOutlined } from "@ant-design/icons";
import { Button, theme } from "antd";
import { useCopy } from "../../api/useCopy";

/** One ready-to-paste snippet, mono and selectable, with a Copy button. */
export function Snippet({ label, text }: { label: string; text: string }) {
  const { token } = theme.useToken();
  const copy = useCopy(text, `${label} snippet`);
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
