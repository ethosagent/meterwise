import { ConfigProvider, theme } from "antd";
import { type ReactNode, useEffect } from "react";
import { useServerPreferences } from "../api/preferences";
import { themeFor } from "./theme";
import {
  hasStoredTheme,
  useApplyModeToDocument,
  usePrefersReducedMotion,
  useResolvedMode,
  useTheme,
} from "./useTheme";

/** Paints the page background from the active tokens, so <body> follows the theme. */
function BodyBackground() {
  const { token } = theme.useToken();
  useEffect(() => {
    document.body.style.background = token.colorBgLayout;
  }, [token.colorBgLayout]);
  return null;
}

/** Applies the server's theme once, and only if this device has not chosen one yet. */
function useAdoptServerTheme() {
  const prefs = useServerPreferences();
  const setMode = useTheme((s) => s.setMode);
  const serverTheme = prefs.data?.theme;
  useEffect(() => {
    if (serverTheme && !hasStoredTheme()) setMode(serverTheme);
  }, [serverTheme, setMode]);
  return prefs.data?.reducedMotion ?? false;
}

/** antd ConfigProvider driven by the persisted theme choice, system preference and motion setting. */
export function ThemeProvider({ children }: { children: ReactNode }) {
  const mode = useResolvedMode();
  const serverReduced = useAdoptServerTheme();
  const systemReduced = usePrefersReducedMotion();
  useApplyModeToDocument(mode);
  return (
    <ConfigProvider theme={themeFor(mode, serverReduced || systemReduced)}>
      <BodyBackground />
      {children}
    </ConfigProvider>
  );
}
