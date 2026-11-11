import {
  type CauseCard,
  type CauseKind,
  type CauseView,
  type ClientRow,
  SAVINGS_CAUSES,
  type SaveList,
  type SaveRow,
  type SavingsCause,
  type SavingsCauses,
  type SavingsDay,
  type SavingsHero,
  type SavingsOverview,
  type Suggestion,
} from "../../contract/generated";
import { parseMicro } from "../../lib/money";

/** The nine causes in the order the API sends them: the six monitors, then trim, router, memory. */
export const CAUSE_ORDER = SAVINGS_CAUSES;
export const MONITOR_ORDER = CAUSE_ORDER.slice(0, 6);

const ZERO = "0.000000";

function micro(v: bigint): string {
  return `${v / 1_000_000n}.${(v % 1_000_000n).toString().padStart(6, "0")}`;
}

const KIND_OF: Record<string, CauseKind> = { trim: "trim", router: "router", memory: "memory" };

/** A cause row with every figure zero; override what the test is about. The three savers start Off. */
export function cause(name: SavingsCause, over: Partial<CauseView> = {}): CauseView {
  const kind = KIND_OF[name] ?? "monitor";
  return {
    cause: name,
    kind,
    mode: kind === "monitor" ? "observe" : "off",
    confidence: name === "memory" ? "likely" : "certain",
    fires: 0,
    savedUsd: ZERO,
    likelySavedUsd: ZERO,
    wouldSaveUsd: ZERO,
    savedTokens: 0,
    likelySavedTokens: 0,
    wouldSaveTokens: 0,
    ...over,
  };
}

/** A Causes-tab card: a cause row plus the most repeated client, tool and path (null when none). */
export function causeCard(name: SavingsCause, over: Partial<CauseCard> = {}): CauseCard {
  return {
    ...cause(name),
    topClient: null,
    topTool: null,
    topPath: null,
    topPathHash: null,
    topRepeatCount: 0,
    ...over,
  };
}

/** The overview's row for a card: the same figures without the Top* detail (R17). */
export function viewOf(card: CauseCard): CauseView {
  const { topClient, topTool, topPath, topPathHash, topRepeatCount, ...view } = card;
  void [topClient, topTool, topPath, topPathHash, topRepeatCount];
  return view;
}

/** A zero-filled UTC day; wouldHavePaid is paid + saved (certain only), as the server computes it. */
export function day(d: string, paid: string, saved: string, likely = ZERO, calls = 0): SavingsDay {
  return {
    day: d,
    paidUsd: paid,
    savedUsd: saved,
    likelySavedUsd: likely,
    wouldHavePaidUsd: micro(parseMicro(paid) + parseMicro(saved)),
    calls,
    sessions: calls === 0 ? 0 : 1,
  };
}

export const DAYS_7: SavingsDay[] = [
  day("2026-11-04", ZERO, ZERO),
  day("2026-11-05", "1.000000", "0.100000", ZERO, 10),
  day("2026-11-06", "1.500000", "0.200000", "0.100000", 14),
  day("2026-11-07", "1.000000", ZERO, ZERO, 8),
  day("2026-11-08", "2.000000", "0.300000", "0.200000", 20),
  day("2026-11-09", "1.500000", "0.200000", "0.100000", 12),
  day("2026-11-10", "2.000000", "0.200000", "0.100000", 16),
];

export const HERO_ACTIVE: SavingsHero = {
  savedUsd: "1.000000",
  likelySavedUsd: "0.500000",
  wouldSaveUsd: "0.200000",
  paidUsd: "9.000000",
  wouldHavePaidUsd: "10.000000",
  savedTokens: 120000,
  likelySavedTokens: 60000,
  wouldSaveTokens: 24000,
  savedBp: 1000,
  unpricedCalls: 0,
};

const HERO_EMPTY: SavingsHero = {
  savedUsd: ZERO,
  likelySavedUsd: ZERO,
  wouldSaveUsd: ZERO,
  paidUsd: ZERO,
  wouldHavePaidUsd: ZERO,
  savedTokens: 0,
  likelySavedTokens: 0,
  wouldSaveTokens: 0,
  savedBp: null,
  unpricedCalls: 0,
};

export const CAUSE_CARDS_ACTIVE: CauseCard[] = [
  causeCard("duplicate_tool_call", {
    mode: "block",
    fires: 5,
    savedUsd: "0.700000",
    savedTokens: 84000,
    topClient: "opencode",
    topTool: "read_file",
    topPath: "src/main.go",
    topRepeatCount: 4,
  }),
  causeCard("repeated_read", {
    mode: "block",
    fires: 3,
    savedUsd: "0.300000",
    savedTokens: 36000,
    topClient: "claude-code",
    topTool: "grep",
    topPathHash: "ab12cd34",
    topRepeatCount: 3,
  }),
  causeCard("no_progress", {
    mode: "note",
    confidence: "likely",
    fires: 4,
    likelySavedUsd: "0.500000",
    likelySavedTokens: 60000,
  }),
  causeCard("step_budget"),
  causeCard("reasoning_loop", { fires: 2, wouldSaveUsd: "0.200000", wouldSaveTokens: 24000 }),
  causeCard("second_guessing"),
  causeCard("trim"),
  causeCard("router"),
  causeCard("memory"),
];

export const CAUSES_ACTIVE: CauseView[] = CAUSE_CARDS_ACTIVE.map(viewOf);

export const OVERVIEW_ACTIVE: SavingsOverview = {
  range: "7d",
  from: "2026-11-04",
  to: "2026-11-10",
  timezone: "UTC",
  state: "active",
  firstCallAt: "2026-11-01T09:00:00Z",
  hero: HERO_ACTIVE,
  days: DAYS_7,
  byCause: CAUSES_ACTIVE,
  upsell: [
    { cause: "reasoning_loop", wouldSaveUsd: "0.200000", wouldSaveTokens: 24000, fires: 2 },
  ],
};

const quietDays = DAYS_7.map((d) => day(d.day, ZERO, ZERO));
const quietCauses = CAUSE_ORDER.map((m) => cause(m));

export const OVERVIEW_NO_CALLS: SavingsOverview = {
  ...OVERVIEW_ACTIVE,
  state: "no_calls",
  firstCallAt: null,
  hero: HERO_EMPTY,
  days: quietDays,
  byCause: quietCauses,
  upsell: [],
};

/** Calls exist, no monitor has acted yet; observe mode has found would-save. */
export const OVERVIEW_COLLECTING: SavingsOverview = {
  ...OVERVIEW_ACTIVE,
  state: "collecting",
  hero: {
    ...HERO_EMPTY,
    paidUsd: "3.000000",
    wouldHavePaidUsd: "3.000000",
    wouldSaveUsd: "0.400000",
    wouldSaveTokens: 48000,
  },
  days: DAYS_7.map((d) => day(d.day, d.paidUsd, ZERO, ZERO, 5)),
  byCause: CAUSE_ORDER.map((m) =>
    m === "duplicate_tool_call"
      ? cause(m, { fires: 3, wouldSaveUsd: "0.400000", wouldSaveTokens: 48000 })
      : cause(m),
  ),
  upsell: [
    { cause: "duplicate_tool_call", wouldSaveUsd: "0.400000", wouldSaveTokens: 48000, fires: 3 },
  ],
};

export const CLIENTS: ClientRow[] = [
  {
    client: "opencode",
    calls: 60,
    sessions: 6,
    blocked: 5,
    spentUsd: "6.000000",
    savedUsd: "0.700000",
    likelySavedUsd: "0.300000",
    savedBp: 1045,
    avgCallsPerSession: 10,
    prevAvgCallsPerSession: 12.5,
  },
  {
    client: "unknown",
    calls: 20,
    sessions: 4,
    blocked: 3,
    spentUsd: "3.000000",
    savedUsd: "0.300000",
    likelySavedUsd: "0.200000",
    savedBp: null,
    avgCallsPerSession: 5,
    prevAvgCallsPerSession: null,
  },
];

export const SUGGESTIONS: Suggestion[] = [
  {
    monitor: "repeated_read",
    tool: "read_file",
    path: "src/main.go",
    pathHash: null,
    count: 4,
    text: "read_file read src/main.go 4 times. Tell the agent to keep what it learned about this file in its notes.",
  },
];

export const CAUSES_RESPONSE: SavingsCauses = {
  range: "7d",
  from: "2026-11-04",
  to: "2026-11-10",
  timezone: "UTC",
  causes: CAUSE_CARDS_ACTIVE,
  clients: CLIENTS,
  suggestions: SUGGESTIONS,
};

export function saveRow(n: number, over: Partial<SaveRow> = {}): SaveRow {
  return {
    eventId: `ev_${n}`,
    sessionId: `sess_${n}`,
    callId: `call_${n}`,
    callIndex: n,
    ts: "2026-11-09T10:00:00Z",
    monitor: "duplicate_tool_call",
    action: "block",
    confidence: "certain",
    estSavedUsd: "0.250000",
    estSavedTokens: 30000,
    client: "opencode",
    model: "gpt-4o",
    ...over,
  };
}

/** Three saves: a certain block, a likely note and a block from a call before a restart (index 0). */
export const SAVES: SaveRow[] = [
  saveRow(1),
  saveRow(2, {
    monitor: "no_progress",
    action: "note",
    confidence: "likely",
    estSavedUsd: "0.100000",
    client: null,
  }),
  saveRow(3, { callIndex: 0, estSavedUsd: "0.050000" }),
];

export const SAVE_LIST: SaveList = { items: SAVES, nextCursor: null };
