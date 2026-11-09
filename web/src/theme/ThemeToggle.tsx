import { DesktopOutlined, MoonOutlined, SunOutlined } from "@ant-design/icons";
import { Segmented } from "antd";
import { DEFAULT_PREFERENCES, useSavePreferences, useServerPreferences } from "../api/preferences";
import type { ThemeChoice } from "../api/types";
import { useTheme } from "./useTheme";

const OPTIONS: { value: ThemeChoice; label: string; icon: React.ReactNode }[] = [
  { value: "light", label: "Light", icon: <SunOutlined /> },
  { value: "dark", label: "Dark", icon: <MoonOutlined /> },
  { value: "system", label: "System", icon: <DesktopOutlined /> },
];

/** Light / dark / system control. Applies locally at once, then saves to the server. */
export function ThemeToggle() {
  const mode = useTheme((s) => s.mode);
  const setMode = useTheme((s) => s.setMode);
  const prefs = useServerPreferences();
  const save = useSavePreferences();
  const current = prefs.data ?? DEFAULT_PREFERENCES;

  return (
    <Segmented<ThemeChoice>
      aria-label="Theme"
      value={mode}
      options={OPTIONS.map((o) => ({
        value: o.value,
        title: o.label,
        label: (
          <span role="img" aria-label={o.label}>
            {o.icon}
          </span>
        ),
      }))}
      onChange={(next) => {
        setMode(next);
        save.mutate({ ...current, theme: next });
      }}
    />
  );
}
