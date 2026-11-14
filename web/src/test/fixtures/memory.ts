import type {
  MemorySettings,
  MemoryStats,
  TraceDetail,
  TraceSummary,
} from "../../contract/generated";

/** The default memory settings: off, and not enabled for want of a key (no memory model is configured). */
export const MEMORY_SETTINGS: MemorySettings = {
  config: { mode: "off", thresholdBp: 8200, maxHintsPerSession: 1, model: "", embedModel: "" },
  enabled: false,
  disabledReason: "no_key",
};

/** Seven days of memory in hint mode: three traces, two hinted sessions, one helpful and one irrelevant rating. */
export const MEMORY_STATS: MemoryStats = {
  range: "7d",
  from: "2026-11-05",
  to: "2026-11-11",
  mode: "hint",
  traces: 3,
  hintedSessions: 2,
  hits: 3,
  observedHits: 0,
  helpful: 1,
  irrelevant: 1,
  likelySavedUsd: "0.125000",
  selfUsageUsdMonth: "0.043000",
  budgetPaused: false,
};

const trace = (n: number, over: Partial<TraceSummary> = {}): TraceSummary => ({
  id: `trace_${n}`,
  sessionId: `sess_${n}`,
  taskSummary: `Fix build ${n}: missing go.sum entry`,
  files: ["go.mod", "go.sum", "cmd/app/main.go"],
  uses: n,
  upvotes: 1,
  downvotes: 0,
  confidenceBp: 8000,
  lastUsedAt: "2026-11-09T10:00:00Z",
  createdAt: "2026-11-01T10:00:00Z",
  ...over,
});

export const TRACES: TraceSummary[] = [1, 2, 3].map((n) => trace(n, { uses: n === 1 ? 2 : n }));

export const TRACE_DETAIL: TraceDetail = {
  ...trace(1, { uses: 2 }),
  observations: ["go build failed with a missing go.sum entry", "go mod tidy fixed it"],
  approach: "Run go mod tidy, then rebuild",
  fix: "go mod tidy",
  signature: {
    repo: "acme/app",
    files: ["go.mod", "go.sum"],
    errors: ["missing go.sum entry"],
    tools: ["bash"],
  },
  helpedSessions: ["sess_7", "sess_9"],
};
