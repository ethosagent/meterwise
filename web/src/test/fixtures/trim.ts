import { vi } from "vitest";
import type { TrimConfig, TrimPreview, TrimStats } from "../../contract/generated";

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

/** An observe-mode week: nothing applied, 0.3 dollars would have been saved. Three classes, never summed. */
export const TRIM_STATS_OBSERVE: TrimStats = {
  range: "7d",
  from: "2026-11-05",
  to: "2026-11-11",
  mode: "observe",
  calls: 40,
  trimmedCalls: 0,
  savedUsd: "0.000000",
  likelySavedUsd: "0.000000",
  wouldSaveUsd: "0.300000",
  trimTokens: 0,
  wouldTrimTokens: 3200,
  byStage: [
    { stage: "repeat_collapse", fires: 0, reverted: 0, tokens: 0, bytesSaved: 0 },
    { stage: "dup_lines", fires: 0, reverted: 0, tokens: 0, bytesSaved: 0 },
    { stage: "log_window", fires: 0, reverted: 0, tokens: 0, bytesSaved: 0 },
    { stage: "json_table", fires: 0, reverted: 0, tokens: 0, bytesSaved: 0 },
    { stage: "schema_trim", fires: 0, reverted: 0, tokens: 0, bytesSaved: 0 },
  ],
  byTool: [],
};

/** A lossless week: certain 2.0, likely 0.4. */
export const TRIM_STATS_ACTED: TrimStats = {
  ...TRIM_STATS_OBSERVE,
  mode: "lossless",
  trimmedCalls: 12,
  savedUsd: "2.000000",
  likelySavedUsd: "0.400000",
  wouldSaveUsd: "0.000000",
  trimTokens: 1400,
  wouldTrimTokens: 0,
  byStage: [
    { stage: "repeat_collapse", fires: 3, reverted: 0, tokens: 300, bytesSaved: 1200 },
    { stage: "dup_lines", fires: 4, reverted: 0, tokens: 400, bytesSaved: 1600 },
    { stage: "log_window", fires: 5, reverted: 0, tokens: 700, bytesSaved: 2800 },
    { stage: "json_table", fires: 0, reverted: 0, tokens: 0, bytesSaved: 0 },
    { stage: "schema_trim", fires: 0, reverted: 0, tokens: 0, bytesSaved: 0 },
  ],
  byTool: [
    { tool: "run_shell", fires: 9, tokens: 1100 },
    { tool: "get_build_log", fires: 3, tokens: 300 },
  ],
};

/** One acted call: sizes and fixed markers, never text. */
export const TRIM_PREVIEW: TrimPreview = {
  callId: "call_11",
  action: "acted",
  bodyBefore: 5800,
  bodyAfter: 1600,
  stages: [
    {
      stage: "dup_lines",
      tool: "run_shell",
      regions: 1,
      bytesBefore: 5800,
      bytesAfter: 2400,
      estTokens: 850,
      reverted: false,
      marker: "line (×N)",
    },
    {
      stage: "log_window",
      tool: "run_shell",
      regions: 1,
      bytesBefore: 2400,
      bytesAfter: 1600,
      estTokens: 150,
      reverted: false,
      marker: "… N lines folded; K errors kept …",
    },
  ],
};

/**
 * Puts the trim stats and preview routes in front of whatever fetch stub is installed (mockApi serves the settings
 * routes; the stats and preview routes are served here). Returns the requests seen.
 */
export function stubTrimRoutes(stats: TrimStats, preview: TrimPreview = TRIM_PREVIEW): string[] {
  const seen: string[] = [];
  const inner = globalThis.fetch;
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    const ok = (b: unknown) =>
      Promise.resolve(
        new Response(JSON.stringify(b), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
      );
    if (/\/trim\/stats\?/.test(path)) {
      seen.push(path);
      return ok(stats);
    }
    if (/\/calls\/[^/]+\/trim$/.test(path)) {
      seen.push(path);
      return ok(preview);
    }
    return inner(input, init);
  });
  return seen;
}
