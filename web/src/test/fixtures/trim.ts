import type { TrimConfig } from "../../contract/generated";

/** The default trim settings as the API returns them (observe, every stage on but schema_trim). */
export const TRIM_CONFIG: TrimConfig = {
  mode: "observe",
  neverTrimTools: [],
  maxFoldPct: 90,
  minGainPct: 2,
  stages: {
    repeat_collapse: true,
    dup_lines: true,
    log_window: true,
    json_table: true,
    schema_trim: false,
  },
};
