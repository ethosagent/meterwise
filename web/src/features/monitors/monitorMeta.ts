import type { MonitorConfig } from "../../contract/generated";

export type MonitorMode = "observe" | "note" | "block";
export type MonitorKey = keyof MonitorConfig;

/** The six monitors in the order the API sends them. */
export const MONITOR_KEYS: readonly MonitorKey[] = [
  "duplicate_tool_call",
  "repeated_read",
  "no_progress",
  "step_budget",
  "reasoning_loop",
  "second_guessing",
];

/** Monitors that stay observe-only in the UI until tuned; the API still accepts every mode (Plan 06 ruling). */
export const OBSERVE_ONLY: ReadonlySet<string> = new Set(["reasoning_loop", "second_guessing"]);
export const OBSERVE_ONLY_HINT = "observe-only until tuned";

export const MODE_LABEL: Record<MonitorMode, string> = {
  observe: "Observe",
  note: "Nudge",
  block: "Block",
};

const LABELS: Record<MonitorKey, { label: string; about: string }> = {
  duplicate_tool_call: {
    label: "Duplicate tool call",
    about: "The agent repeats the same tool call with the same arguments.",
  },
  repeated_read: {
    label: "Repeated read",
    about: "The agent re-reads a file it has already read.",
  },
  no_progress: {
    label: "No progress",
    about: "Consecutive steps come out nearly the same.",
  },
  step_budget: {
    label: "Step budget",
    about: "A session runs past a step budget.",
  },
  reasoning_loop: {
    label: "Reasoning loop",
    about: "The agent's reasoning keeps going around the same text.",
  },
  second_guessing: {
    label: "Second guessing",
    about: "The agent keeps reversing its own decision.",
  },
};

function isKey(name: string): name is MonitorKey {
  return name in LABELS;
}

/** A readable name for a monitor, falling back to the wire name for one this build does not know. */
export function monitorLabel(name: string): string {
  return isKey(name) ? LABELS[name].label : name;
}

export function monitorAbout(name: string): string {
  return isKey(name) ? LABELS[name].about : "";
}

export function isMode(v: string): v is MonitorMode {
  return v === "observe" || v === "note" || v === "block";
}

export interface Threshold {
  field: string;
  label: string;
  kind: "number" | "boolean";
}

/** The tunable thresholds of each monitor, named as the PATCH body spells them. */
export const THRESHOLDS: Record<MonitorKey, readonly Threshold[]> = {
  duplicate_tool_call: [
    { field: "maxRepeats", label: "Max repeats", kind: "number" },
    { field: "lookBack", label: "Look back (calls)", kind: "number" },
    { field: "fuzzyPct", label: "Fuzzy match (%)", kind: "number" },
    { field: "abab", label: "Catch A-B-A-B loops", kind: "boolean" },
  ],
  repeated_read: [
    { field: "minReads", label: "Reads before it counts", kind: "number" },
    { field: "blockAfter", label: "Block after", kind: "number" },
  ],
  no_progress: [{ field: "similarityPct", label: "Similarity (%)", kind: "number" }],
  step_budget: [{ field: "maxSteps", label: "Max steps", kind: "number" }],
  reasoning_loop: [{ field: "shingleTokens", label: "Shingle size (tokens)", kind: "number" }],
  second_guessing: [{ field: "minPhrases", label: "Phrases before it counts", kind: "number" }],
};
