import { type ThemeConfig, theme } from "antd";

// The only file in web/src that may hold colour literals (docs/decisions/0008).
// Components read tokens through theme.useToken(); a test fails on a hex anywhere else.
const palette = {
  light: {
    primary: "#2f6bdc",
    success: "#0f7a50",
    warning: "#9a5b00",
    error: "#cf3350",
    layout: "#f4f6fa",
    container: "#ffffff",
    elevated: "#ffffff",
    border: "#dfe4ee",
    text: "#1a2236",
    textSecondary: "#5b667d",
  },
  dark: {
    primary: "#4f8cff",
    success: "#2fd48a",
    warning: "#ffb347",
    error: "#ff5c7a",
    layout: "#0b0f17",
    container: "#111726",
    elevated: "#182034",
    border: "#243049",
    text: "#e6eaf2",
    textSecondary: "#8d97ad",
  },
  sider: { bg: "#0e1424", subBg: "#0a0f1b", trigger: "#162036", selected: "#1d2a47" },
} as const;

/** Layout dimensions in one place so components carry no pixel literals. */
export const SIDER_WIDTH = 248;
export const SIDER_COLLAPSED_WIDTH = 72;
export const HEADER_HEIGHT = 56;
const FONT_STACK =
  "system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif";

export type ResolvedMode = "light" | "dark";

function base(mode: ResolvedMode): ThemeConfig {
  const p = palette[mode];
  return {
    algorithm: mode === "dark" ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: p.primary,
      colorInfo: p.primary,
      colorSuccess: p.success,
      colorWarning: p.warning,
      colorError: p.error,
      colorTextLightSolid: mode === "dark" ? palette.dark.layout : palette.light.container,
      colorBgLayout: p.layout,
      colorBgContainer: p.container,
      colorBgElevated: p.elevated,
      colorBorder: p.border,
      colorBorderSecondary: p.border,
      colorText: p.text,
      colorTextSecondary: p.textSecondary,
      borderRadius: 8,
      fontFamily: FONT_STACK,
    },
    components: {
      Layout: {
        headerBg: p.container,
        headerHeight: HEADER_HEIGHT,
        headerPadding: "0 24px",
        footerBg: p.layout,
        bodyBg: p.layout,
        siderBg: palette.sider.bg,
        triggerBg: palette.sider.trigger,
      },
    },
  };
}

export const lightTheme: ThemeConfig = base("light");
export const darkTheme: ThemeConfig = base("dark");

/** The sider is dark in both themes: its subtree gets this config through a nested provider. */
export const siderTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: palette.dark.primary,
    // The parent dark theme sets a dark value here; the sider's dark menu needs a light one.
    colorTextLightSolid: palette.dark.text,
    colorBgContainer: palette.sider.bg,
    colorBgElevated: palette.dark.elevated,
    colorBorder: palette.dark.border,
    colorText: palette.dark.text,
    colorTextSecondary: palette.dark.textSecondary,
    borderRadius: 8,
    fontFamily: FONT_STACK,
  },
  components: {
    Layout: { siderBg: palette.sider.bg, triggerBg: palette.sider.trigger },
    Menu: {
      darkItemBg: palette.sider.bg,
      darkSubMenuItemBg: palette.sider.subBg,
      darkItemSelectedBg: palette.sider.selected,
      darkPopupBg: palette.sider.subBg,
    },
  },
};

/** Theme for a resolved mode; `reducedMotion` switches antd's animations off. */
export function themeFor(mode: ResolvedMode, reducedMotion: boolean): ThemeConfig {
  const cfg = mode === "dark" ? darkTheme : lightTheme;
  return reducedMotion ? { ...cfg, token: { ...cfg.token, motion: false } } : cfg;
}
