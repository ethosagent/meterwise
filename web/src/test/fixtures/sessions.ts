import type {
  Call,
  MonitorConfig,
  MonitorEvent,
  MonitorStats,
  MonitorStatsRow,
  Session,
  SessionDetail,
} from "../../contract/generated";
import { MONITOR_ORDER } from "./savings";

export function session(n: number, over: Partial<Session> = {}): Session {
  return {
    id: `sess_${n}`,
    workspaceId: "ws_1",
    source: "header",
    startedAt: `2026-11-09T10:0${n}:00Z`,
    lastSeenAt: `2026-11-09T10:${12 + n}:00Z`,
    closedAt: `2026-11-09T10:${12 + n}:00Z`,
    calls: 4,
    inputTokens: 52000,
    outputTokens: 3100,
    costUsd: "0.800000",
    savedUsd: "0.250000",
    likelySavedUsd: "0.100000",
    outcome: "completed",
    client: "opencode",
    ...over,
  };
}

export const SESSIONS: Session[] = [
  session(1),
  session(2, {
    client: null,
    outcome: "abandoned",
    savedUsd: "0.000000",
    likelySavedUsd: "0.000000",
  }),
  session(3, { client: "claude-code", outcome: "open", closedAt: null, costUsd: null }),
];

function call(n: number, over: Partial<Call> = {}): Call {
  return {
    id: `call_${n}`,
    callIndex: n,
    startedAt: `2026-11-09T10:0${n}:00Z`,
    provider: "openai",
    model: "gpt-4o",
    wire: "openai-chat",
    stream: true,
    status: 200,
    finish: "stop",
    inputTokens: 10000 + n * 2000,
    outputTokens: 600,
    cachedTokens: 0,
    reasoningTokens: 0,
    costUsd: "0.200000",
    ttfbMs: 300,
    durationMs: 2100,
    toolCalls: ["read_file"],
    blocked: false,
    notesInjected: 0,
    ...over,
  };
}

export const CALLS: Call[] = [
  call(1),
  call(2),
  call(3, { blocked: true, costUsd: "0.000000", toolCalls: [] }),
  call(4, { notesInjected: 1, toolCalls: ["grep"] }),
];

/** A block that points back at call 2, and a note whose matched call predates a restart (index 0). */
export const EVENT_BLOCK: MonitorEvent = {
  id: "ev_1",
  callId: "call_3",
  callIndex: 3,
  ts: "2026-11-09T10:03:00Z",
  monitor: "duplicate_tool_call",
  mode: "block",
  action: "block",
  evidence: { matchedCalls: [2], tool: "read_file", path: "src/main.go", count: 3, threshold: 2 },
  estSavedTokens: 30000,
  estSavedUsd: "0.250000",
  confidence: "certain",
  feedback: null,
};

export const EVENT_NOTE: MonitorEvent = {
  id: "ev_2",
  callId: "call_4",
  callIndex: 4,
  ts: "2026-11-09T10:04:00Z",
  monitor: "repeated_read",
  mode: "note",
  action: "note",
  evidence: { matchedCalls: [0], tool: "grep", pathHash: "ab12cd34ef567890", count: 3 },
  estSavedTokens: 12000,
  estSavedUsd: "0.100000",
  confidence: "likely",
  feedback: "wrong",
};

export const DETAIL: SessionDetail = {
  session: session(1),
  calls: CALLS,
  truncated: false,
  monitorEvents: [EVENT_BLOCK, EVENT_NOTE],
  insight: {
    repeats: [
      {
        monitor: "repeated_read",
        tool: "read_file",
        path: "src/main.go",
        pathHash: null,
        count: 4,
        text: "read_file read src/main.go 4 times.",
      },
    ],
    suggestion: "Tell the agent to keep notes about src/main.go instead of re-reading it.",
  },
};

export const MONITOR_CONFIG: MonitorConfig = {
  duplicate_tool_call: { mode: "observe", maxRepeats: 3, lookBack: 20, fuzzyPct: 90, abab: true },
  repeated_read: { mode: "note", minReads: 2, blockAfter: 4 },
  no_progress: { mode: "observe", similarityPct: 90 },
  step_budget: { mode: "observe", maxSteps: 50 },
  reasoning_loop: { mode: "observe", shingleTokens: 8 },
  second_guessing: { mode: "observe", minPhrases: 3 },
};

function statsRow(monitor: string, over: Partial<MonitorStatsRow> = {}): MonitorStatsRow {
  return {
    monitor,
    mode: "observe",
    fires: 0,
    blocked: 0,
    noted: 0,
    observed: 0,
    savedTokens: 0,
    likelySavedTokens: 0,
    wouldSaveTokens: 0,
    savedUsd: "0.000000",
    likelySavedUsd: "0.000000",
    wouldSaveUsd: "0.000000",
    feedbackCorrect: 0,
    feedbackWrong: 0,
    falsePositiveRate: null,
    ...over,
  };
}

export const MONITOR_STATS: MonitorStats = {
  range: "7d",
  savedUsd: "0.300000",
  likelySavedUsd: "0.100000",
  wouldSaveUsd: "0.700000",
  monitors: MONITOR_ORDER.map((m) => {
    if (m === "duplicate_tool_call") {
      return statsRow(m, {
        fires: 6,
        observed: 6,
        wouldSaveTokens: 84000,
        wouldSaveUsd: "0.700000",
      });
    }
    if (m === "repeated_read") {
      return statsRow(m, {
        mode: "note",
        fires: 4,
        noted: 4,
        likelySavedUsd: "0.100000",
        savedUsd: "0.300000",
        feedbackCorrect: 1,
        feedbackWrong: 2,
        falsePositiveRate: 0.66,
      });
    }
    return statsRow(m);
  }),
};
