import { DesktopOutlined, MoonOutlined, SunOutlined } from "@ant-design/icons";
import { App, Segmented } from "antd";
import { useSavePreferences } from "../api/preferences";
import type { Theme } from "../contract/generated";
import { useTheme } from "./useTheme";

const OPTIONS: { value: Theme; label: string; icon: React.ReactNode }[] = [
  { value: "light", label: "Light", icon: <SunOutlined /> },
  { value: "dark", label: "Dark", icon: <MoonOutlined /> },
  { value: "system", label: "System", icon: <DesktopOutlined /> },
];

/** Light / dark / system control. Applies locally at once, then saves to the server. */
export function ThemeToggle() {
  const mode = useTheme((s) => s.mode);
  const setMode = useTheme((s) => s.setMode);
  const save = useSavePreferences();
  const { message } = App.useApp();

  return (
    <Segmented<Theme>
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
        save.mutate(
          { theme: next },
          {
            onError: () =>
              message.error("Could not save your theme. It applies on this device only."),
          },
        );
      }}
    />
  );
}
